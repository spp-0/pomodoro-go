package ui

import (
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
)

type Options struct {
	Width            int
	Height           int
	AutoCloseSeconds int
	TopMost          bool
	Position         string // center / top-left / top-right / bottom-left / bottom-right
}

// data 是渲染到弹窗里的数据。
type data struct {
	Title      string
	Message    string
	Kind       string
	TimeText   string
	Quote      string
	Author     string
	Source     string
	AutoClose  int
	PayloadB64 string
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

	d := data{
		Title:     strings.TrimSpace(e.Title),
		Message:   strings.TrimSpace(e.Message),
		Kind:      e.Kind,
		TimeText:  e.At.In(time.Local).Format("2006-01-02 15:04:05"),
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

	// 关键：把"强制置顶 + 抢焦点 + 任务栏闪烁"排到 webview2 的 UI 线程。
	// 必须发生在 w.Run() 启动事件循环之后才真正执行；w.Dispatch 是异步的，
	// 任务会被 Run() 启动后取出执行。
	// 无条件执行：用户需求"每次弹窗都显示"，不再依赖 opt.TopMost 配置。
	w.Dispatch(func() { forceForegroundPopup(uintptr(w.Window())) })

	done := make(chan struct{})
	closed := make(chan struct{})
	w.Bind("closeWindow", func() {
		// 1) 关闭 webview 窗口
		w.Destroy()
		// 2) 通知 Run() 退出（不调 Terminate，避免它强行结束宿主进程）
		select {
		case <-closed:
		default:
			close(closed)
		}
	})

	w.SetHtml(html)
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
    .hidden{ display:none; }
  </style>
</head>
<body>
  <div class="card">
    <div class="glow"></div>
    <h2 class="title">
      <span class="badge" id="kind">{{.Kind}}</span>
      <span id="title">{{.Title}}</span>
    </h2>
    <div class="msg" id="msg">{{.Message}}</div>
    <div class="quote">
      <div class="text" id="qtext">{{.Quote}}</div>
      <div class="meta" id="qmeta">— {{.Author}}{{if .Source}} · {{.Source}}{{end}}</div>
    </div>
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
    if (d && d.kind)    document.getElementById("kind").textContent   = d.kind;
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

// ---------- Windows 强制置顶 + 抢焦点 ----------

// forceForegroundPopup 把指定 HWND 的窗口拉到最前面、抢焦点，并闪烁任务栏
// 图标兜底。解决"其他程序最大化时弹窗只出现在任务栏、屏幕上看不到"。
// 仅在 Windows 上有实际效果；其它平台直接 no-op。
//
// 关键点：
//  1. WindowStyle 库默认 WS_EX_TOPMOST=0 没置位，所以窗口很容易被全屏程序
//     盖住。这里用 SetWindowPos(HWND_TOPMOST) 动态补上 topmost 标志。
//  2. Windows 7+ 有"前台锁"（Foreground Lock Timeout），普通进程的
//     SetForegroundWindow 会被系统静默拒掉。必须先 LockSetForegroundWindow(2)
//     解锁，再 SetForegroundWindow 才能抢到焦点。
//  3. 即便焦点没抢到（比如用户当前正在打字），也调 FlashWindowEx 闪任务栏
//     图标，用户能从任务栏感知到。
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
		HWND_TOPMOST   = ^uintptr(0) // -1 as uintptr
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

	// 1) 若窗口被最小化，先还原；否则保险地 ShowWindow 一下，确保可见
	if isIconic, _, _ := procIsIconic.Call(hwnd); isIconic != 0 {
		procShowWindow.Call(hwnd, SW_RESTORE)
	} else {
		procShowWindow.Call(hwnd, SW_SHOWNORMAL)
	}

	// 2) 解除前台锁，让 SetForegroundWindow 真正生效
	procLockSetFG.Call(LSFW_UNLOCK)

	// 3) 设 TOPMOST 并刷到 Z 序最顶
	procSetWindowPos.Call(
		hwnd, HWND_TOPMOST, 0, 0, 0, 0,
		SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW,
	)
	// 4) 抢焦点
	procBringToTop.Call(hwnd)
	procSetFG.Call(hwnd)

	// 5) 兜底：闪烁任务栏图标（即使焦点被系统拒掉也能让用户看到）
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
	// FLASHW_ALL | FLASHW_TIMERNOFG：直到窗口变成前台才停止闪烁
	fi.dwFlags = FLASHW_ALL | FLASHW_TIMERNOFG
	fi.uCount = 0 // 0 表示持续闪烁直到前台
	fi.dwTimeout = 0
	procFlash.Call(uintptr(unsafe.Pointer(&fi)))
}
