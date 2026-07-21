package ui

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Krakinsight/go-webview2"

	"pomodoro-notifier/internal/config"
	"pomodoro-notifier/internal/logging"
	"pomodoro-notifier/internal/scheduler"
	"pomodoro-notifier/internal/stats"
)

// settingsView 是设置窗口与 JS 之间交换的数据结构（仅包含可编辑字段 + 展示用统计）。
type settingsView struct {
	PomodoroEnabled  bool                   `json:"pomodoro_enabled"`
	WorkMinutes      int                    `json:"work_minutes"`
	BreakMinutes     int                    `json:"break_minutes"`
	TimepointEnabled bool                   `json:"timepoint_enabled"`
	Times            []config.TimepointItem `json:"times"`
	SoundEnabled     bool                   `json:"sound_enabled"`
	WeatherEnabled   bool                   `json:"weather_enabled"`
	WeatherCity      string                 `json:"weather_city"`
	Position         string                 `json:"position"`
	QuoteURL         string                 `json:"quote_url"`
	QuoteTimeout     string                 `json:"quote_timeout"`
	Autostart        bool                   `json:"autostart"`
	StatsToday       int                    `json:"stats_today"`
	StatsDates       []string               `json:"stats_dates"`
	StatsLast7       []stats.DayStat        `json:"stats_last7"`
}

// ShowSettings 打开设置窗口（在非 Windows 平台为空操作）。
// onSaved 在保存成功后回调，用于做配置外的副作用（如同步开机自启注册表）。
// statsStore 用于展示统计（可为 nil）。
func ShowSettings(configPath string, sched *scheduler.ServiceScheduler, logger *logging.Logger, onSaved func(config.AppConfig), statsStore *stats.Store) {
	if runtime.GOOS != "windows" {
		return
	}
	go func() {
		runtime.LockOSThread()
		openSettingsWindow(configPath, sched, logger, onSaved, statsStore)
	}()
}

func openSettingsWindow(configPath string, sched *scheduler.ServiceScheduler, logger *logging.Logger, onSaved func(config.AppConfig), statsStore *stats.Store) {
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Printf("[settings] load config failed: %v", err)
		return
	}
	view := settingsViewFromConfig(cfg, statsStore)
	payload, _ := json.Marshal(view)
	b64 := base64.StdEncoding.EncodeToString(payload)

	w, err := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "PomodoroNotifier 设置",
			Width:  680,
			Height: 720,
			Center: true,
		},
	})
	if err != nil || w == nil {
		logger.Printf("[settings] open window failed: %v", err)
		return
	}

	// 保存：接收 JS 传来的 JSON 字符串，校验后原子写盘并热重载。
	w.Bind("saveSettings", func(jsonStr string) string {
		msg, ok := applySettings(configPath, sched, logger, jsonStr, onSaved)
		if !ok {
			return msg
		}
		return ""
	})
	w.Bind("closeSettings", func() {
		w.Destroy()
	})

	w.SetHtml(renderSettings(b64))
	w.Run()
}

func settingsViewFromConfig(cfg config.AppConfig, statsStore *stats.Store) settingsView {
	v := settingsView{
		PomodoroEnabled:  cfg.Pomodoro.Enabled,
		WorkMinutes:      cfg.Pomodoro.WorkMinutes,
		BreakMinutes:     cfg.Pomodoro.BreakMinutes,
		TimepointEnabled: cfg.Timepoint.Enabled,
		Times:            cfg.Timepoint.Times,
		SoundEnabled:     cfg.Popup.Sound.Enabled,
		WeatherEnabled:   cfg.Weather.Enabled,
		WeatherCity:      cfg.Weather.City,
		Position:         cfg.Popup.Position,
		QuoteURL:         cfg.QuoteAPI.URL,
		QuoteTimeout:     cfg.QuoteAPI.Timeout,
		Autostart:        cfg.Autostart,
	}
	if statsStore != nil {
		v.StatsToday = statsStore.Today().Pomodoros
		v.StatsLast7 = statsStore.Last7()
		dates := make([]string, 0, 7)
		today := time.Now()
		for i := 6; i >= 0; i-- {
			dates = append(dates, today.AddDate(0, 0, -i).Format("01-02"))
		}
		v.StatsDates = dates
	}
	return v
}

// applySettings 解析表单、校验、原子保存并热重载；返回 (错误信息, 是否成功)。
func applySettings(configPath string, sched *scheduler.ServiceScheduler, logger *logging.Logger, jsonStr string, onSaved func(config.AppConfig)) (string, bool) {
	var v settingsView
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return "表单数据解析失败: " + err.Error(), false
	}
	if v.WorkMinutes <= 0 || v.BreakMinutes <= 0 {
		return "工作/休息分钟数必须大于 0", false
	}
	for _, it := range v.Times {
		if _, err := time.Parse("15:04", strings.TrimSpace(it.Time)); err != nil {
			return "时间点格式错误（应为 HH:MM）: " + it.Time, false
		}
	}
	if _, err := time.ParseDuration(v.QuoteTimeout); err != nil {
		return "诗词超时格式错误（如 1500ms）: " + v.QuoteTimeout, false
	}
	if v.Position == "" {
		v.Position = "bottom-right"
	}
	city := strings.TrimSpace(v.WeatherCity)
	if v.WeatherEnabled && city == "" {
		city = "北京"
	}

	// 基于当前文件配置覆盖可编辑字段，保留 work_days / work_start 等未暴露字段。
	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}
	cfg.Pomodoro.Enabled = v.PomodoroEnabled
	cfg.Pomodoro.WorkMinutes = v.WorkMinutes
	cfg.Pomodoro.BreakMinutes = v.BreakMinutes
	cfg.Timepoint.Enabled = v.TimepointEnabled
	cfg.Timepoint.Times = v.Times
	cfg.Popup.Sound.Enabled = v.SoundEnabled
	cfg.Popup.Position = v.Position
	cfg.Weather.Enabled = v.WeatherEnabled
	cfg.Weather.City = city
	cfg.QuoteAPI.URL = v.QuoteURL
	cfg.QuoteAPI.Timeout = v.QuoteTimeout
	cfg.Autostart = v.Autostart

	if err := saveConfigAtomic(configPath, cfg); err != nil {
		return "保存失败: " + err.Error(), false
	}
	if loc, lerr := cfg.Location(); lerr == nil {
		sched.UpdateConfig(cfg, loc)
	} else {
		sched.UpdateConfig(cfg, nil)
	}
	if onSaved != nil {
		onSaved(cfg)
	}
	logger.Printf("[settings] saved")
	return "", true
}

// saveConfigAtomic 先写临时文件再 rename，避免写一半损坏 config.json。
func saveConfigAtomic(path string, cfg config.AppConfig) error {
	cfg.ApplyDefaults()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

const settingsTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>设置</title>
  <style>
    html,body{height:100%;margin:0;font-family: ui-sans-serif, system-ui, "PingFang SC", "Microsoft YaHei", Arial;
      background: linear-gradient(160deg, #0b1220, #111a2e); color:#e5e7eb; -webkit-font-smoothing:antialiased;}
    .wrap{height:100%;overflow:auto;box-sizing:border-box;padding:18px 22px 22px;}
    h2{font-size:18px;margin:0 0 14px;}
    .sec{margin:14px 0;padding:12px 14px;border:1px solid #334155;border-radius:12px;background:rgba(15,23,42,.6);}
    .sec h3{font-size:14px;margin:0 0 10px;color:#a5b4fc;}
    label{display:flex;align-items:center;gap:8px;font-size:13px;margin:6px 0;}
    .row{display:flex;align-items:center;gap:10px;margin:6px 0;flex-wrap:wrap;}
    input[type=text],input[type=number],select{background:#0f172a;color:#e5e7eb;border:1px solid #334155;
      border-radius:8px;padding:6px 8px;font-size:13px;}
    input[type=number]{width:80px;}
    input[type=text]{flex:1;}
    .tp-row{display:flex;gap:6px;margin:6px 0;}
    .tp-time{width:90px;}
    .tp-title{width:140px;}
    .tp-msg{flex:1;}
    .btn{cursor:pointer;border:1px solid rgba(96,165,250,.45);background:linear-gradient(180deg,rgba(96,165,250,.2),rgba(96,165,250,.1));
      color:#dbeafe;padding:7px 14px;border-radius:8px;font-size:13px;}
    .btn:hover{background:rgba(96,165,250,.3);}
    .btn.sub{border-color:rgba(165,180,252,.35);background:rgba(30,41,59,.55);color:#c7d2fe;padding:5px 10px;}
    .btn.sub:hover{background:rgba(165,180,252,.18);}
    .tp-del{background:transparent;border:1px solid #475569;color:#94a3b8;border-radius:8px;cursor:pointer;width:30px;}
    .actions{display:flex;gap:10px;justify-content:flex-end;margin-top:16px;}
    #err{display:none;color:#fca5a5;font-size:13px;margin-top:10px;padding:8px 10px;border:1px solid #7f1d1d;
      background:rgba(127,29,29,.25);border-radius:8px;}
    .hint{font-size:12px;color:#94a3b8;margin-top:4px;}
    .stat-big{font-size:14px;margin:4px 0 8px;}
    .stat-big b{color:#fcd34d;font-size:18px;}
    .stat-7{display:flex;flex-direction:column;gap:4px;}
    .stat-7 .srow{display:flex;justify-content:space-between;font-size:12px;color:#cbd5e1;
      padding:4px 8px;border-radius:6px;background:rgba(30,41,59,.5);}
    .stat-7 .srow .p{color:#fcd34d;}
  </style>
</head>
<body>
  <div class="wrap">
    <h2>⚙️ PomodoroNotifier 设置</h2>

    <div class="sec">
      <h3>番茄钟</h3>
      <label><input type="checkbox" id="pomodoro_enabled"> 启用番茄钟循环</label>
      <div class="row"><span>工作</span><input type="number" id="work_minutes" min="1"> <span>分钟</span>
        <span style="margin-left:12px;">休息</span><input type="number" id="break_minutes" min="1"> <span>分钟</span></div>
      <div class="row">
        <span>快捷预设</span>
        <button class="btn sub preset" data-w="25" data-b="5">25 / 5</button>
        <button class="btn sub preset" data-w="50" data-b="10">50 / 10</button>
        <button class="btn sub preset" data-w="90" data-b="20">90 / 20</button>
      </div>
    </div>

    <div class="sec">
      <h3>时间点提醒</h3>
      <label><input type="checkbox" id="timepoint_enabled"> 启用指定时间点提醒</label>
      <div id="tp-list"></div>
      <button class="btn sub" id="add-tp">+ 添加时间点</button>
      <div class="hint">时间格式 HH:MM；标题/内容留空则使用下方默认文案。</div>
    </div>

    <div class="sec">
      <h3>提醒音</h3>
      <label><input type="checkbox" id="sound_enabled"> 弹窗时播放提醒音</label>
    </div>

    <div class="sec">
      <h3>天气（弹窗内显示）</h3>
      <label><input type="checkbox" id="weather_enabled"> 弹窗内显示天气</label>
      <div class="row"><span>城市</span><input type="text" id="weather_city" placeholder="如 北京 / 上海"></div>
      <div class="hint">使用 Open-Meteo 免费接口，无需密钥；断网时自动隐藏。</div>
    </div>

    <div class="sec">
      <h3>弹窗与诗词</h3>
      <div class="row"><span>弹窗位置</span>
        <select id="position">
          <option value="center">居中</option>
          <option value="top-left">左上</option>
          <option value="top-right">右上</option>
          <option value="bottom-left">左下</option>
          <option value="bottom-right">右下</option>
        </select>
      </div>
      <div class="row"><span>诗词API</span><input type="text" id="quote_url"></div>
      <div class="row"><span>超时</span><input type="text" id="quote_timeout" style="width:120px;"></div>
    </div>

    <div class="sec">
      <h3>统计</h3>
      <div class="stat-big">🍅 今日完成 <b id="stat-today">0</b> 个番茄钟</div>
      <div class="hint">最近 7 天</div>
      <div id="stat-7" class="stat-7"></div>
    </div>

    <div class="sec">
      <h3>其他</h3>
      <label><input type="checkbox" id="autostart"> 开机自启</label>
    </div>

    <div id="err"></div>
    <div class="actions">
      <button class="btn sub" id="cancel">取消</button>
      <button class="btn" id="save">保存</button>
    </div>
  </div>
<script>
(function(){
  var b64 = "{{.PayloadB64}}";
  function decodeB64(b64){
    var json = atob(b64);
    var bytes = new Uint8Array(json.length);
    for (var i=0;i<json.length;i++) bytes[i]=json.charCodeAt(i);
    return new TextDecoder("utf-8").decode(bytes);
  }
  function addRow(time, title, message){
    var row = document.createElement("div"); row.className = "tp-row";
    var t = document.createElement("input"); t.className="tp-time"; t.placeholder="HH:MM"; t.value=time||"";
    var ti = document.createElement("input"); ti.className="tp-title"; ti.placeholder="标题(可选)"; ti.value=title||"";
    var m = document.createElement("input"); m.className="tp-msg"; m.placeholder="内容(可选)"; m.value=message||"";
    var del = document.createElement("button"); del.className="tp-del"; del.textContent="×";
    del.onclick = function(){ row.remove(); };
    row.appendChild(t); row.appendChild(ti); row.appendChild(m); row.appendChild(del);
    document.getElementById("tp-list").appendChild(row);
  }
  try {
    var cfg = JSON.parse(decodeB64(b64));
    document.getElementById("pomodoro_enabled").checked = !!cfg.pomodoro_enabled;
    document.getElementById("work_minutes").value = cfg.work_minutes || 25;
    document.getElementById("break_minutes").value = cfg.break_minutes || 5;
    document.getElementById("timepoint_enabled").checked = !!cfg.timepoint_enabled;
    document.getElementById("sound_enabled").checked = !!cfg.sound_enabled;
    document.getElementById("weather_enabled").checked = !!cfg.weather_enabled;
    document.getElementById("weather_city").value = cfg.weather_city || "";
    document.getElementById("position").value = cfg.position || "bottom-right";
    document.getElementById("quote_url").value = cfg.quote_url || "";
    document.getElementById("quote_timeout").value = cfg.quote_timeout || "1500ms";
    document.getElementById("autostart").checked = !!cfg.autostart;
    document.getElementById("stat-today").textContent = cfg.stats_today || 0;
    (cfg.times||[]).forEach(function(t){ addRow(t.time, t.title, t.message); });
    var s7 = document.getElementById("stat-7");
    var dates = cfg.stats_dates || [];
    var last7 = cfg.stats_last7 || [];
    for (var i=0;i<dates.length;i++){
      var row = document.createElement("div"); row.className="srow";
      var d = document.createElement("span"); d.textContent = dates[i];
      var p = document.createElement("span"); p.className="p";
      var st = last7[i] || {pomodoros:0};
      p.textContent = (st.pomodoros||0) + " 🍅";
      row.appendChild(d); row.appendChild(p);
      s7.appendChild(row);
    }
  } catch(e){
    document.getElementById("err").textContent = "配置解析失败: " + e;
    document.getElementById("err").style.display = "block";
  }
  document.getElementById("add-tp").onclick = function(){ addRow("", "", ""); };
  var presets = document.querySelectorAll(".preset");
  for (var p=0;p<presets.length;p++){
    presets[p].onclick = function(){
      document.getElementById("work_minutes").value = parseInt(this.getAttribute("data-w"),10);
      document.getElementById("break_minutes").value = parseInt(this.getAttribute("data-b"),10);
    };
  }
  document.getElementById("cancel").onclick = function(){ window.closeSettings(); };
  document.getElementById("save").onclick = function(){
    var times = [];
    var rows = document.querySelectorAll("#tp-list .tp-row");
    for (var i=0;i<rows.length;i++){
      var r = rows[i];
      var time = r.querySelector(".tp-time").value.trim();
      if (time !== "") times.push({ time: time, title: r.querySelector(".tp-title").value, message: r.querySelector(".tp-msg").value });
    }
    var data = {
      pomodoro_enabled: document.getElementById("pomodoro_enabled").checked,
      work_minutes: parseInt(document.getElementById("work_minutes").value,10)||0,
      break_minutes: parseInt(document.getElementById("break_minutes").value,10)||0,
      timepoint_enabled: document.getElementById("timepoint_enabled").checked,
      times: times,
      sound_enabled: document.getElementById("sound_enabled").checked,
      weather_enabled: document.getElementById("weather_enabled").checked,
      weather_city: document.getElementById("weather_city").value,
      position: document.getElementById("position").value,
      quote_url: document.getElementById("quote_url").value,
      quote_timeout: document.getElementById("quote_timeout").value,
      autostart: document.getElementById("autostart").checked
    };
    var p = window.saveSettings(JSON.stringify(data));
    function done(err){
      var box = document.getElementById("err");
      if (err) { box.textContent = err; box.style.display = "block"; }
      else { window.closeSettings(); }
    }
    if (p && typeof p.then === "function") { p.then(done); } else { done(p); }
  };
})();
</script>
</body>
</html>`

func renderSettings(b64 string) string {
	return strings.Replace(settingsTemplate, "{{.PayloadB64}}", b64, 1)
}
