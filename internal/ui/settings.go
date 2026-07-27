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
	"pomodoro-notifier/internal/i18n"
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
	Language         string                 `json:"language"` // 界面语言
	StatsToday       int                    `json:"stats_today"`
	StatsDates       []string               `json:"stats_dates"`
	StatsLast7       []stats.DayStat      `json:"stats_last7"`
	// 以下用于前端本地化（不参与回写）
	Langs  []LangOption       `json:"langs"`
	I18n   map[string]string `json:"i18n"`
}

// LangOption 是语言下拉项。
type LangOption struct {
	Value string `json:"value"`
	Name  string `json:"name"`
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
			Title:  i18n.T(i18n.Lang(cfg.Language), "settings.title"),
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
	lang := i18n.Lang(cfg.Language)
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
		Language:         cfg.Language,
		Langs:           buildLangs(),
		I18n:            i18n.Dict(lang),
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

// buildLangs 构造语言下拉项（顺序同 i18n.Supported）。
func buildLangs() []LangOption {
	out := make([]LangOption, 0, len(i18n.Supported()))
	for _, l := range i18n.Supported() {
		out = append(out, LangOption{Value: string(l), Name: i18n.Name(l)})
	}
	return out
}

// applySettings 解析表单、校验、原子保存并热重载；返回 (错误信息, 是否成功)。
func applySettings(configPath string, sched *scheduler.ServiceScheduler, logger *logging.Logger, jsonStr string, onSaved func(config.AppConfig)) (string, bool) {
	var v settingsView
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return i18n.T(i18n.Lang(v.Language), "settings.err.parse_form") + err.Error(), false
	}
	if v.WorkMinutes <= 0 || v.BreakMinutes <= 0 {
		return i18n.T(i18n.Lang(v.Language), "settings.err.minutes"), false
	}
	for _, it := range v.Times {
		if _, err := time.Parse("15:04", strings.TrimSpace(it.Time)); err != nil {
			return i18n.T(i18n.Lang(v.Language), "settings.err.timepoint_fmt") + it.Time, false
		}
	}
	if _, err := time.ParseDuration(v.QuoteTimeout); err != nil {
		return i18n.T(i18n.Lang(v.Language), "settings.err.timeout_fmt") + v.QuoteTimeout, false
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
	// 语言切换时，对「仍是旧语言默认文案」的提醒字段，重落地为新语言默认。
	oldLang := i18n.Lang(cfg.Language)
	newLang := i18n.Norm(i18n.Lang(v.Language))
	relocalizeDefault := func(field *string, key string) {
		if *field == i18n.T(oldLang, key) {
			*field = i18n.T(newLang, key)
		}
	}
	relocalizeDefault(&cfg.Pomodoro.WorkText, "default.pomodoro_work")
	relocalizeDefault(&cfg.Pomodoro.BreakText, "default.pomodoro_break")
	relocalizeDefault(&cfg.Timepoint.Title, "default.timepoint_title")
	relocalizeDefault(&cfg.Timepoint.Message, "default.timepoint_message")

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
	cfg.Language = string(newLang)

	if err := saveConfigAtomic(configPath, cfg); err != nil {
		return i18n.T(newLang, "settings.err.save") + err.Error(), false
	}
	if loc, lerr := cfg.Location(); lerr == nil {
		sched.UpdateConfig(cfg, loc)
	} else {
		sched.UpdateConfig(cfg, nil)
	}
	if onSaved != nil {
		onSaved(cfg)
	}
	logger.Printf("[settings] saved (lang=%s)", cfg.Language)
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
<html lang="zh">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>settings</title>
  <style>
    html,body{height:100%;margin:0;font-family: ui-sans-serif, system-ui, "PingFang SC", "Microsoft YaHei", "Microsoft JhengHei", "Malgun Gothic", "Yu Gothic", "Meiryo", Arial;
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
    <h2 id="title"></h2>

    <div class="sec">
      <h3 id="sec-pomodoro"></h3>
      <label><input type="checkbox" id="pomodoro_enabled"> <span id="lbl-pomodoro-enabled"></span></label>
      <div class="row"><span id="lbl-work"></span><input type="number" id="work_minutes" min="1"> <span id="lbl-minutes"></span>
        <span style="margin-left:12px;" id="lbl-break"></span><input type="number" id="break_minutes" min="1"> <span id="lbl-minutes2"></span></div>
      <div class="row">
        <span id="lbl-presets"></span>
        <button class="btn sub preset" data-w="25" data-b="5">25 / 5</button>
        <button class="btn sub preset" data-w="50" data-b="10">50 / 10</button>
        <button class="btn sub preset" data-w="90" data-b="20">90 / 20</button>
      </div>
    </div>

    <div class="sec">
      <h3 id="sec-timepoint"></h3>
      <label><input type="checkbox" id="timepoint_enabled"> <span id="lbl-timepoint-enabled"></span></label>
      <div id="tp-list"></div>
      <button class="btn sub" id="add-tp"></button>
      <div class="hint" id="hint-timepoint"></div>
    </div>

    <div class="sec">
      <h3 id="sec-sound"></h3>
      <label><input type="checkbox" id="sound_enabled"> <span id="lbl-sound-enabled"></span></label>
    </div>

    <div class="sec">
      <h3 id="sec-weather"></h3>
      <label><input type="checkbox" id="weather_enabled"> <span id="lbl-weather-enabled"></span></label>
      <div class="row"><span id="lbl-city"></span><input type="text" id="weather_city"></div>
      <div class="hint" id="hint-weather"></div>
    </div>

    <div class="sec">
      <h3 id="sec-popup-quote"></h3>
      <div class="row"><span id="lbl-position"></span>
        <select id="position">
          <option value="center" id="pos-center"></option>
          <option value="top-left" id="pos-topleft"></option>
          <option value="top-right" id="pos-topright"></option>
          <option value="bottom-left" id="pos-bottomleft"></option>
          <option value="bottom-right" id="pos-bottomright"></option>
        </select>
      </div>
      <div class="row"><span id="lbl-quote-api"></span><input type="text" id="quote_url"></div>
      <div class="row"><span id="lbl-quote-timeout"></span><input type="text" id="quote_timeout" style="width:120px;"></div>
    </div>

    <div class="sec">
      <h3 id="sec-language"></h3>
      <div class="row"><span id="lbl-language"></span>
        <select id="language"></select>
      </div>
      <div class="hint" id="hint-language"></div>
    </div>

    <div class="sec">
      <h3 id="sec-stats"></h3>
      <div class="stat-big" id="stat-big"></div>
      <div class="hint" id="hint-stats"></div>
      <div id="stat-7" class="stat-7"></div>
    </div>

    <div class="sec">
      <h3 id="sec-other"></h3>
      <label><input type="checkbox" id="autostart"> <span id="lbl-autostart"></span></label>
    </div>

    <div id="err"></div>
    <div class="actions">
      <button class="btn sub" id="cancel"></button>
      <button class="btn" id="save"></button>
    </div>
  </div>
<script>
(function(){
  var I18N = {};
  var LANGS = [];
  var b64 = "{{.PayloadB64}}";
  function t(k){ return (I18N && I18N[k]) ? I18N[k] : k; }
  function decodeB64(b64){
    var json = atob(b64);
    var bytes = new Uint8Array(json.length);
    for (var i=0;i<json.length;i++) bytes[i]=json.charCodeAt(i);
    return new TextDecoder("utf-8").decode(bytes);
  }
  function addRow(time, title, message){
    var row = document.createElement("div"); row.className = "tp-row";
    var t = document.createElement("input"); t.className="tp-time"; t.placeholder="HH:MM"; t.value=time||"";
    var ti = document.createElement("input"); ti.className="tp-title"; ti.placeholder=t('settings.timepoint.add'); ti.value=title||"";
    var m = document.createElement("input"); m.className="tp-msg"; m.placeholder=t('settings.timepoint.add'); m.value=message||"";
    var del = document.createElement("button"); del.className="tp-del"; del.textContent="×";
    del.onclick = function(){ row.remove(); };
    row.appendChild(t); row.appendChild(ti); row.appendChild(m); row.appendChild(del);
    document.getElementById("tp-list").appendChild(row);
  }
  try {
    var cfg = JSON.parse(decodeB64(b64));
    I18N = cfg.i18n || {};
    LANGS = cfg.langs || [];
    // 标题与分区
    document.getElementById("title").textContent = t('settings.title');
    document.getElementById("sec-pomodoro").textContent = t('settings.sec.pomodoro');
    document.getElementById("sec-timepoint").textContent = t('settings.sec.timepoint');
    document.getElementById("sec-sound").textContent = t('settings.sec.sound');
    document.getElementById("sec-weather").textContent = t('settings.sec.weather');
    document.getElementById("sec-popup-quote").textContent = t('settings.sec.popup_quote');
    document.getElementById("sec-language").textContent = t('settings.language');
    document.getElementById("sec-stats").textContent = t('settings.sec.stats');
    document.getElementById("sec-other").textContent = t('settings.sec.other');
    // 标签
    document.getElementById("lbl-pomodoro-enabled").textContent = t('settings.pomodoro.enabled');
    document.getElementById("lbl-work").textContent = t('settings.pomodoro.work');
    document.getElementById("lbl-minutes").textContent = t('settings.pomodoro.minutes');
    document.getElementById("lbl-break").textContent = t('settings.pomodoro.break');
    document.getElementById("lbl-minutes2").textContent = t('settings.pomodoro.minutes');
    document.getElementById("lbl-presets").textContent = t('settings.pomodoro.presets');
    document.getElementById("lbl-timepoint-enabled").textContent = t('settings.timepoint.enabled');
    document.getElementById("lbl-sound-enabled").textContent = t('settings.sound.enabled');
    document.getElementById("lbl-weather-enabled").textContent = t('settings.weather.enabled');
    document.getElementById("lbl-city").textContent = t('settings.weather.city');
    document.getElementById("lbl-position").textContent = t('settings.popup.position');
    document.getElementById("lbl-quote-api").textContent = t('settings.quote.api');
    document.getElementById("lbl-quote-timeout").textContent = t('settings.quote.timeout');
    document.getElementById("lbl-language").textContent = t('settings.language');
    document.getElementById("lbl-autostart").textContent = t('settings.other.autostart');
    document.getElementById("hint-timepoint").textContent = t('settings.timepoint.hint');
    document.getElementById("hint-weather").textContent = t('settings.weather.hint');
    document.getElementById("hint-language").textContent = t('settings.language.hint');
    // 占位符
    document.getElementById("weather_city").placeholder = t('settings.weather.placeholder');
    // 位置选项
    document.getElementById("pos-center").textContent = t('settings.pos.center');
    document.getElementById("pos-topleft").textContent = t('settings.pos.topleft');
    document.getElementById("pos-topright").textContent = t('settings.pos.topright');
    document.getElementById("pos-bottomleft").textContent = t('settings.pos.bottomleft');
    document.getElementById("pos-bottomright").textContent = t('settings.pos.bottomright');
    // 按钮
    document.getElementById("add-tp").textContent = t('settings.timepoint.add');
    document.getElementById("cancel").textContent = t('settings.btn.cancel');
    document.getElementById("save").textContent = t('settings.btn.save');
    // 语言下拉
    var sel = document.getElementById("language");
    for (var i=0;i<LANGS.length;i++){
      var o = document.createElement("option");
      o.value = LANGS[i].value; o.textContent = LANGS[i].name;
      sel.appendChild(o);
    }
    sel.value = cfg.language || "zh-CN";
    // 表单回填
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
    document.getElementById("stat-big").textContent = "🍅 " + t('settings.stats.today') + " " + (cfg.stats_today||0) + " " + t('settings.stats.pomodoros');
    document.getElementById("hint-stats").textContent = t('settings.stats.last7');
    (cfg.times||[]).forEach(function(tp){ addRow(tp.time, tp.title, tp.message); });
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
    document.getElementById("err").textContent = t('settings.err.parse_cfg') + e;
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
      autostart: document.getElementById("autostart").checked,
      language: document.getElementById("language").value
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
