package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Krakinsight/go-webview2"

	"pomodoro-notifier/internal/quote"
	"pomodoro-notifier/internal/scheduler"
	"pomodoro-notifier/internal/weather"
)

type Options struct {
	Width            int
	Height           int
	AutoCloseSeconds int
	TopMost          bool
	Position         string // center / top-left / top-right / bottom-left / bottom-right
	Loc             *time.Location // 弹窗时间显示所用时区；nil 时回退 time.Local
	SoundEnabled    bool           // 是否播放提醒音
	SoundFile       string         // 自定义 wav 路径；空=系统默认提示音
	OnSnooze        func(minutes int) // 非 nil 时显示“稍后提醒”按钮
	WeatherEnabled  bool           // 是否在弹窗内显示天气
	WeatherCity     string         // 天气城市
}

// data 是渲染到弹窗里的数据。
type data struct {
	Title         string
	Message       string
	Kind          string
	Badge         string // 面向用户的中文标签（由 Kind 映射）
	TimeText      string
	Quote         string
	Author        string
	Source        string
	AutoClose     int
	PayloadB64    string
	SnoozeEnabled bool   // 是否显示“稍后提醒”按钮
	WeatherEmoji  string // 天气 emoji（无则空）
	WeatherCity   string
	WeatherTemp   string // 如 "26°C"
	WeatherText   string // 如 "多云"
}

// friendlyBadge 把内部事件类型映射为面向用户的中文标签。
func friendlyBadge(kind string) string {
	switch kind {
	case "pomodoro_break_start":
		return "休息提醒"
	case "pomodoro_break_end":
		return "开始专注"
	case "timepoint":
		return "定时提醒"
	case "manual":
		return "手动提醒"
	case "test":
		return "测试"
	default:
		return "提醒"
	}
}

func ShowPopup(e scheduler.PopupEvent, q quote.Quote, opt Options) error {
	if opt.Width <= 0 {
		opt.Width = 560
	}
	if opt.Height <= 0 {
		opt.Height = 340
	}
	if opt.AutoCloseSeconds <= 0 {
		opt.AutoCloseSeconds = 20
	}
	tz := opt.Loc
	if tz == nil {
		tz = time.Local
	}

	d := data{
		Title:     strings.TrimSpace(e.Title),
		Message:   strings.TrimSpace(e.Message),
		Kind:      e.Kind,
		Badge:     friendlyBadge(e.Kind),
		TimeText:  e.At.In(tz).Format("2006-01-02 15:04:05"),
		Quote:     q.Text,
		Author:    q.Author,
		Source:    q.Source,
		AutoClose: opt.AutoCloseSeconds,
	}
	if d.Title == "" {
		d.Title = "提醒"
	}
	if d.Message == "" {
		d.Message = "到点啦，起来活动一下。"
	}
	if d.Quote == "" {
		d.Quote = quote.Fallback().Text
		d.Author = quote.Fallback().Author
	}

	d.SnoozeEnabled = opt.OnSnooze != nil
	// 天气：在弹窗线程内异步取（带超时），失败静默忽略，不影响提醒本身。
	if opt.WeatherEnabled && strings.TrimSpace(opt.WeatherCity) != "" {
		wctx, wcancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer wcancel()
		if w, werr := weather.Fetch(wctx, opt.WeatherCity); werr == nil {
			d.WeatherEmoji = w.Emoji
			d.WeatherCity = w.City
			d.WeatherTemp = weather.TempString(w.Temperature)
			d.WeatherText = w.Text
		}
	}

	// 序列化一份 JSON 通过 base64 注入，页面里解码后做兜底渲染（避免模板转义问题）
	payload, _ := json.Marshal(d)
	d.PayloadB64 = base64.StdEncoding.EncodeToString(payload)

	html, err := render(d)
	if err != nil {
		return err
	}

	loc, center := computeLocation(opt)

	w, err := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:    d.Title,
			Width:    uint(opt.Width),
			Height:   uint(opt.Height),
			Center:   center,
			Location: loc,
		},
	})
	if err != nil || w == nil {
		return err
	}
	w.Dispatch(func() { forceForegroundPopup(uintptr(w.Window())) })

	done := make(chan struct{})
	closed := make(chan struct{})
	closeOnce := func() {
		// 1) 关闭 webview 窗口
		w.Destroy()
		// 2) 通知 Run() 退出（不调 Terminate，避免它强行结束宿主进程）
		select {
		case <-closed:
		default:
			close(closed)
		}
	}
	w.Bind("closeWindow", func() {
		closeOnce()
	})
	if opt.OnSnooze != nil {
		// 稍后提醒：记录延迟后重新触发，然后关闭当前窗口。
		w.Bind("snooze", func(minutes float64) {
			if opt.OnSnooze != nil {
				opt.OnSnooze(int(minutes))
			}
			closeOnce()
		})
	}

	w.SetHtml(html)
	if opt.SoundEnabled {
		playNotificationSound(opt.SoundFile)
	}
	go func() {
		<-done
	}()
	w.Run()

	// 兜底：让 closeWindow 回调把窗口关掉后，再让 ShowPopup 返回
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		// 2s 还没收到 close 信号（用户没点按钮），强制 Destroy
		w.Destroy()
	}
	close(done)
	return nil
}

func render(d data) (string, error) {
	tpl := template.Must(template.New("p").Parse(pageTemplate))
	var b strings.Builder
	if err := tpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// computeLocation 根据 opt.Position 计算窗口左上角坐标。
// 返回 (*Location, center)；center=true 时由 webview2 库自己居中。
// 不用 webview2 库的"负坐标 = 距 work-area 边缘"语义（实测对 Y 负值
// 解释为"屏幕顶部上方"），改用 Windows SystemParametersInfoSPI_GETWORKAREA
// 取工作区，算绝对像素坐标，避开任务栏。
func computeLocation(opt Options) (*webview2.Location, bool) {
	const margin = 16
	w, h := opt.Width, opt.Height
	if w <= 0 {
		w = 560
	}
	if h <= 0 {
		h = 340
	}
	pos := strings.ToLower(strings.TrimSpace(opt.Position))
	if pos == "center" || pos == "" {
		return nil, true
	}
	wa, ok := getWorkArea()
	if !ok {
		// 拿不到工作区就退回到左上角
		return &webview2.Location{X: int32(margin), Y: int32(margin)}, false
	}
	var x, y int32
	w32, h32 := int32(w), int32(h)
	switch pos {
	case "top-left":
		x = wa.Left + int32(margin)
		y = wa.Top + int32(margin)
	case "top-right":
		x = wa.Right - w32 - int32(margin)
		y = wa.Top + int32(margin)
	case "bottom-left":
		x = wa.Left + int32(margin)
		y = wa.Bottom - h32 - int32(margin)
	default: // bottom-right
		x = wa.Right - w32 - int32(margin)
		y = wa.Bottom - h32 - int32(margin)
	}
	return &webview2.Location{X: x, Y: y}, false
}

const pageTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.Title}}</title>
  <style>
    :root{
      --bg1:#0b1220;
      --bg2:#111a2e;
      --card:#0f172aee;
      --text:#e5e7eb;
      --muted:#a5b4fc;
      --accent:#60a5fa;
      --accent2:#a78bfa;
      --border:#334155;
      --shadow: 0 24px 80px rgba(0,0,0,.55);
    }
    html,body{height:100%; margin:0; font-family: ui-sans-serif, system-ui, "Segoe UI", "PingFang SC", "Microsoft YaHei", Arial;}
    body{
      display:flex; align-items:center; justify-content:center;
      background: radial-gradient(1200px 600px at 20% 10%, rgba(96,165,250,.28), transparent 60%),
                  radial-gradient(900px 500px at 80% 20%, rgba(167,139,250,.22), transparent 55%),
                  linear-gradient(160deg, var(--bg1), var(--bg2));
      color:var(--text);
      -webkit-font-smoothing: antialiased;
      user-select:none;
      overflow:hidden;
    }
    .card{
      width: 88%;
      max-width: 520px;
      border:1px solid var(--border);
      border-radius: 18px;
      background: linear-gradient(180deg, rgba(15,23,42,.92), rgba(2,6,23,.88));
      box-shadow: var(--shadow);
      padding: 22px 22px 18px 22px;
      position:relative;
      overflow:hidden;
    }
    .glow{
      position:absolute; inset:-40px -40px auto auto; width:200px; height:200px;
      background: radial-gradient(circle at 30% 30%, rgba(96,165,250,.35), transparent 60%);
      filter: blur(4px);
      pointer-events:none;
    }
    .title{
      font-size: 18px;
      letter-spacing:.2px;
      margin:0 0 8px 0;
      display:flex; align-items:center; gap:10px;
    }
    .badge{
      font-size:12px; color:#c7d2fe;
      border:1px solid rgba(165,180,252,.35);
      padding:3px 10px; border-radius: 999px;
      background: rgba(30,41,59,.55);
    }
    .msg{
      font-size: 15px; line-height: 1.7;
      color:#e2e8f0;
      margin: 6px 0 14px 0;
    }
    .quote{
      margin-top: 8px;
      padding: 14px 14px;
      border-radius: 12px;
      background: linear-gradient(180deg, rgba(99,102,241,.10), rgba(168,85,247,.08));
      border:1px solid rgba(165,180,252,.18);
    }
    .quote .text{
      font-size: 14px; line-height:1.7; color:#e5e7eb;
      font-style: italic;
    }
    .quote .meta{
      margin-top:8px; font-size:12px; color:#a5b4fc; text-align:right;
    }
    .footer{
      margin-top: 14px;
      display:flex; align-items:center; justify-content:space-between;
      font-size:12px; color:#94a3b8;
    }
    .btn{
      cursor:pointer;
      border:1px solid rgba(96,165,250,.45);
      background: linear-gradient(180deg, rgba(96,165,250,.20), rgba(96,165,250,.10));
      color:#dbeafe;
      padding:6px 14px; border-radius: 8px; font-size:13px;
    }
    .btn:hover{ background: rgba(96,165,250,.30); }
    .timer{
      font-variant-numeric: tabular-nums;
      color:#fcd34d;
    }
    .weather{
      display:flex; align-items:center; gap:8px; margin:2px 0 10px 0;
      font-size:13px; color:#e2e8f0;
      padding:7px 10px; border-radius:10px;
      background: linear-gradient(180deg, rgba(56,189,248,.12), rgba(56,189,248,.06));
      border:1px solid rgba(125,211,252,.22);
    }
    .weather .w-emoji{ font-size:18px; }
    .weather .w-temp{ font-weight:600; color:#bae6fd; }
    .weather .w-text{ color:#93c5fd; }
    .snooze{
      display:flex; align-items:center; gap:8px; margin:10px 0 4px 0; flex-wrap:wrap;
    }
    .snooze-label{ font-size:12px; color:#94a3b8; }
    .snooze-btn{ background: rgba(30,41,59,.55); border-color: rgba(165,180,252,.30); color:#c7d2fe; }
    .snooze-btn:hover{ background: rgba(165,180,252,.18); }
    .hidden{ display:none; }
  </style>
</head>
<body>
  <div class="card">
    <div class="glow"></div>
    <h2 class="title">
      <span class="badge" id="kind">{{.Badge}}</span>
      <span id="title">{{.Title}}</span>
    </h2>
    {{if .WeatherEmoji}}
    <div class="weather" id="weather">
      <span class="w-emoji">{{.WeatherEmoji}}</span>
      <span class="w-city">{{.WeatherCity}}</span>
      <span class="w-temp">{{.WeatherTemp}}</span>
      <span class="w-text">{{.WeatherText}}</span>
    </div>
    {{end}}
    <div class="msg" id="msg">{{.Message}}</div>
    <div class="quote">
      <div class="text" id="qtext">{{.Quote}}</div>
      <div class="meta" id="qmeta">— {{.Author}}{{if .Source}} · {{.Source}}{{end}}</div>
    </div>
    {{if .SnoozeEnabled}}
    <div class="snooze" id="snooze">
      <span class="snooze-label">稍后提醒</span>
      <button class="btn snooze-btn" data-min="5">5 分钟</button>
      <button class="btn snooze-btn" data-min="10">10 分钟</button>
      <button class="btn snooze-btn" data-min="15">15 分钟</button>
    </div>
    {{end}}
    <div class="footer">
      <span id="time">{{.TimeText}}</span>
      <span>
        <span class="timer" id="timer">{{.AutoClose}}</span>s 后自动关闭
        <button class="btn" id="close">知道了</button>
      </span>
    </div>
  </div>
<script>
(function(){
  // 通过 base64 注入的兜底数据，防止模板里出现空字符串
  var b64 = "{{.PayloadB64}}";
  try {
    var json = atob(b64);
    var bytes = new Uint8Array(json.length);
    for (var i = 0; i < json.length; i++) bytes[i] = json.charCodeAt(i);
    var text = new TextDecoder("utf-8").decode(bytes);
    var d = JSON.parse(text);
    if (d && d.title)   document.getElementById("title").textContent = d.title;
    if (d && d.message) document.getElementById("msg").textContent    = d.message;
    var badge = d && (d.Badge || d.badge || d.Kind || d.kind);
    if (badge) document.getElementById("kind").textContent = badge;
    if (d && d.quote)   document.getElementById("qtext").textContent  = d.quote;
    if (d && d.author)  document.getElementById("qmeta").textContent  = "— " + d.author + (d.source ? " · " + d.source : "");
    if (d && d.time_text) document.getElementById("time").textContent = d.time_text;
  } catch (e) {
    document.getElementById("msg").textContent = "弹窗数据解析失败: " + e;
  }

  var sec = parseInt(document.getElementById("timer").textContent, 10) || 20;
  var t = setInterval(function(){
    sec--;
    if (sec <= 0) { clearInterval(t); try { window.closeWindow(); } catch(e){} ; return; }
    document.getElementById("timer").textContent = sec;
  }, 1000);

  document.getElementById("close").addEventListener("click", function(){
    try { window.closeWindow(); } catch(e){}
  });
  var snoozeBtns = document.querySelectorAll(".snooze-btn");
  for (var i=0;i<snoozeBtns.length;i++){
    snoozeBtns[i].addEventListener("click", function(){
      var min = parseInt(this.getAttribute("data-min"),10) || 5;
      try { window.snooze(min); } catch(e){ try { window.closeWindow(); } catch(_){} }
    });
  }
  document.addEventListener("keydown", function(e){
    if (e.key === "Escape" || e.key === "Enter") {
      try { window.closeWindow(); } catch(e){}
    }
  });
})();
</script>
</body>
</html>`

// ---------- Windows 工作区（work area）辅助 ----------

type winRect struct {
	Left, Top, Right, Bottom int32
}

// getWorkArea 通过 SystemParametersInfoW(SPI_GETWORKAREA) 取主屏工作区。
// 失败时返回 (zero, false)，调用方应退回到默认位置。
func getWorkArea() (winRect, bool) {
	const SPI_GETWORKAREA = 0x0030
	var r winRect
	h, err := syscall.LoadDLL("user32.dll")
	if err != nil {
		return r, false
	}
	proc, err := h.FindProc("SystemParametersInfoW")
	if err != nil {
		return r, false
	}
	ret, _, _ := proc.Call(
		SPI_GETWORKAREA,
		0,
		uintptr(unsafe.Pointer(&r)),
		0,
	)
	if ret == 0 {
		return r, false
	}
	return r, true
}

func forceForegroundPopup(hwnd uintptr) {
	if runtime.GOOS != "windows" {
		return
	}
	if hwnd == 0 {
		return
	}
	const (
		SW_SHOWNORMAL  = 1
		SW_RESTORE     = 9
		HWND_TOPMOST   = ^uintptr(0)
		SWP_NOMOVE     = 0x0002
		SWP_NOSIZE     = 0x0001
		SWP_SHOWWINDOW = 0x0040
		LSFW_UNLOCK    = 2
		FLASHW_ALL     = 3
		FLASHW_TIMERNOFG = 12
	)
	user32 := syscall.NewLazyDLL("user32.dll")
	procIsIconic := user32.NewProc("IsIconic")
	procShowWindow := user32.NewProc("ShowWindow")
	procSetWindowPos := user32.NewProc("SetWindowPos")
	procBringToTop := user32.NewProc("BringWindowToTop")
	procLockSetFG := user32.NewProc("LockSetForegroundWindow")
	procSetFG := user32.NewProc("SetForegroundWindow")
	procFlash := user32.NewProc("FlashWindowEx")

	if isIconic, _, _ := procIsIconic.Call(hwnd); isIconic != 0 {
		procShowWindow.Call(hwnd, SW_RESTORE)
	} else {
		procShowWindow.Call(hwnd, SW_SHOWNORMAL)
	}

	procLockSetFG.Call(LSFW_UNLOCK)

	procSetWindowPos.Call(
		hwnd, HWND_TOPMOST, 0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW,
	)
	procBringToTop.Call(hwnd)
	procSetFG.Call(hwnd)

	type flashInfo struct {
		cbSize    uint32
		hwnd      uintptr
		dwFlags   uint32
		uCount    uint32
		dwTimeout uint32
	}
	var fi flashInfo
	fi.cbSize = uint32(unsafe.Sizeof(fi))
	fi.hwnd = hwnd
	fi.dwFlags = FLASHW_ALL | FLASHW_TIMERNOFG
	fi.uCount = 0
	fi.dwTimeout = 0
	procFlash.Call(uintptr(unsafe.Pointer(&fi)))
}

// playNotificationSound 播放提醒音。
// file 为空时使用系统默认提示音（MessageBeep）；否则播放指定 wav 文件。
// 非 Windows 平台为空操作。
func playNotificationSound(file string) {
	if runtime.GOOS != "windows" {
		return
	}
	const (
		SND_FILENAME = 0x00020000
		SND_ASYNC    = 0x0001
		MB_ICONINFORMATION = 0x00000040
	)
	user32 := syscall.NewLazyDLL("user32.dll")
	procMessageBeep := user32.NewProc("MessageBeep")
	if strings.TrimSpace(file) == "" {
		procMessageBeep.Call(uintptr(MB_ICONINFORMATION))
		return
	}
	mm := syscall.NewLazyDLL("winmm.dll")
	procPlaySound := mm.NewProc("PlaySoundW")
	p, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		procMessageBeep.Call(uintptr(MB_ICONINFORMATION))
		return
	}
	procPlaySound.Call(uintptr(unsafe.Pointer(p)), 0, SND_FILENAME|SND_ASYNC)
}
