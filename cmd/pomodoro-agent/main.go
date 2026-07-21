package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gogpu/systray"

	"pomodoro-notifier/internal/config"
	"pomodoro-notifier/internal/logging"
	"pomodoro-notifier/internal/quote"
	"pomodoro-notifier/internal/scheduler"
	"pomodoro-notifier/internal/ui"
)

//go:embed assets/tray.png
var trayWorkPNG []byte

//go:embed assets/tray_break.png
var trayBreakPNG []byte

//go:embed assets/tray_pause.png
var trayPausePNG []byte

// popupJob 是 UI dispatcher 消费的任务。
type popupJob struct {
	e    scheduler.PopupEvent
	opts ui.Options
}

// popupQueue 是 UI dispatcher 唯一的输入端，emit 把弹窗任务投到这里。
// UI dispatcher 在它启动的 goroutine（与主托盘消息循环同一个 goroutine）
// 同步调用 ui.ShowPopup，绕过 webview2 库对"调用者线程"的隐式要求。
var popupQueue = make(chan popupJob, 16)

// webviewWarned 保证“弹窗渲染失败”只提示一次，避免消息框刷屏。
var webviewWarned atomic.Bool

func main() {
	var (
		configPath  string
		testMode    bool
	)
	flag.StringVar(&configPath, "config", "", "config path (default: <exe-dir>/config.json)")
	flag.BoolVar(&testMode, "test", false, "popup a test reminder and exit (still GUI subsystem)")
	flag.Parse()

	// 1) 解析配置
	path, err := resolveConfigPath(configPath)
	if err != nil {
		fail("resolve config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = config.DefaultConfig()
			cfg.LogFile = "pomodoro.log"
			if saveErr := config.Save(path, cfg); saveErr != nil {
				fail("save default config: %v", saveErr)
			}
		} else {
			// 配置解析失败：降级为默认配置 + 弹窗提示，但不退出，
			// 保证工具始终能启动（提醒类工具“起不来”比“弹错”更糟）。
			showInfo("配置有误", "配置文件存在错误，已使用默认配置。\n请检查: "+path+"\n\n"+err.Error())
			cfg = config.DefaultConfig()
			cfg.LogFile = "pomodoro.log"
		}
	}
	cfg.LogFile = filepath.Join(filepath.Dir(path), filepath.Base(cfg.LogFile))
	logger := logging.New(cfg.LogFile)
	logger.Printf("start config=%s", path)

	// 启动时按配置同步开机自启注册表项（幂等）。
	if err := setAutostart(cfg.Autostart); err != nil {
		logger.Printf("setAutostart: %v", err)
	}

	// 2) 测试模式
	if testMode {
		e := scheduler.PopupEvent{
			Kind:    "test",
			Title:   "测试弹窗",
			Message: "如果你看到这段文字 + 下面这句诗词，说明弹窗工作正常。",
			At:      time.Now(),
		}
		q := quote.Fallback()
		_ = ui.ShowPopup(e, q, ui.Options{
			Width:            cfg.Popup.Width,
			Height:           cfg.Popup.Height,
			AutoCloseSeconds: cfg.Popup.AutoCloseSeconds,
			TopMost:          cfg.Popup.TopMost,
			Position:         cfg.Popup.Position,
			SoundEnabled:     cfg.Popup.Sound.Enabled,
			SoundFile:        cfg.Popup.Sound.File,
		})
		return
	}

	// 3) 正常模式：托盘 + 调度
	loc, err := cfg.Location()
	if err != nil {
		logger.Printf("bad timezone: %v, use local", err)
		loc = time.Local
	}
	quoteTimeout, err := cfg.QuoteTimeout()
	if err != nil {
		quoteTimeout = 3 * time.Second
	}

	var paused atomic.Bool
	sched := scheduler.New(cfg, loc, nil) // 先创建，emit 后面赋上
	scheduler.TickDebugf = func(format string, args ...any) {
		logger.Printf("[tick] "+format, args...)
	}
	emit := func(e scheduler.PopupEvent) {
		logger.Printf("[emit] called kind=%s title=%q", e.Kind, e.Title)
		if paused.Load() {
			logger.Printf("[emit] suppressed by paused flag")
			return
		}
		// 关键：每次触发都从 sched 取最新 cfg，确保热重载生效
		cur := sched.CurrentConfig()
		loc, _ := cur.Location()
		logger.Printf("[emit] popup: pos=%s size=%dx%d autoclose=%ds", cur.Popup.Position, cur.Popup.Width, cur.Popup.Height, cur.Popup.AutoCloseSeconds)
		// 把弹窗任务投递到 UI dispatcher 队列。
		// UI dispatcher 是个独立的"锁定 OS 线程"的 goroutine，保证 webview2 库的
		// 内部 goroutine 与 ShowPopup 在同一个 OS 线程，避开主线程消息循环的
		// 隐式要求。emit 自身不阻塞调度 tick；诗词在 dispatcher 里异步取，
		// 因此即使断网/慢网，调度 tick 也不会被卡住。
		select {
		case popupQueue <- popupJob{e: e, opts: ui.Options{
			Width:            cur.Popup.Width,
			Height:           cur.Popup.Height,
			AutoCloseSeconds: cur.Popup.AutoCloseSeconds,
			TopMost:          cur.Popup.TopMost,
			Position:         cur.Popup.Position,
			Loc:              loc,
			SoundEnabled:     cur.Popup.Sound.Enabled,
			SoundFile:        cur.Popup.Sound.File,
		}}:
			logger.Printf("[emit] enqueued to UI dispatcher")
		default:
			logger.Printf("[emit] popup queue full, dropping event %s", e.Kind)
		}
	}
	sched.SetEmitter(emit)

	// 配置热重载
	go watchConfig(path, sched, logger)

	// UI dispatcher：锁定 OS 线程，按顺序消费弹窗任务。
	// webview2 库的 Run() 必须阻塞在同一个 OS 线程。
	startUIDispatcher(logger, sched, quoteTimeout)

	// 调度主循环
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for now := range ticker.C {
			sched.Tick(now)
		}
	}()

	// 启动托盘消息循环（阻塞直到退出）
	tray := systray.New()
	applyTrayState(tray, scheduler.StateIdle, &paused)
	tray.SetTooltip("🍅 PomodoroNotifier")
	menu := buildMenu(tray, path, sched, logger, &paused)
	tray.SetMenu(menu)

	// 注册 scheduler 状态切换 → 切托盘图标
	sched.SetStateListener(func(st scheduler.State) {
		display := st
		if paused.Load() {
			display = scheduler.StateIdle
		}
		applyTrayState(tray, display, &paused)
	})

	tray.OnClick(func() { emitManual(sched) })
	tray.Show()
	if err := tray.Run(); err != nil {
		logger.Printf("tray run: %v", err)
	}
	logger.Printf("exit")
}

// applyTrayState 根据状态切换托盘图标。
func applyTrayState(tray *systray.SystemTray, st scheduler.State, paused *atomic.Bool) {
	var png []byte
	var tip string
	switch st {
	case scheduler.StateWork:
		png = trayWorkPNG
		tip = "🍅 PomodoroNotifier - 专注中"
	case scheduler.StateBreak:
		png = trayBreakPNG
		tip = "🍅 PomodoroNotifier - 休息中"
	default:
		png = trayPausePNG
		if paused != nil && paused.Load() {
			tip = "🍅 PomodoroNotifier - 已暂停"
		} else {
			tip = "🍅 PomodoroNotifier - 待命中"
		}
	}
	tray.SetIcon(png)
	tray.SetTooltip(tip)
}

func resolveConfigPath(flagPath string) (string, error) {
	if flagPath != "" {
		return filepath.Clean(flagPath), nil
	}
	return config.DefaultConfigPath()
}

func buildMenu(tray *systray.SystemTray, cfgPath string, sched *scheduler.ServiceScheduler, logger *logging.Logger, paused *atomic.Bool) *systray.Menu {
	m := systray.NewMenu()
	m.Add("立即弹一次", func() { emitManual(sched) })
	m.Add("设置…", func() { openSettings(cfgPath, sched, logger) })
	m.AddSeparator()
	m.AddCheckbox("暂停调度（点击切换）", false, func() {
		if paused.Load() {
			paused.Store(false)
			logger.Printf("resume")
			// 恢复后图标按 scheduler 当前实际状态
			applyTrayState(tray, sched.State(), paused)
		} else {
			paused.Store(true)
			sched.Pause()
			logger.Printf("paused")
			applyTrayState(tray, scheduler.StateIdle, paused)
		}
	})
	m.AddCheckbox("声音", sched.CurrentConfig().Popup.Sound.Enabled, func() {
		cur := sched.CurrentConfig()
		enabled := !cur.Popup.Sound.Enabled
		if c, err := config.Load(cfgPath); err == nil {
			c.Popup.Sound.Enabled = enabled
			if err := config.Save(cfgPath, c); err != nil {
				showInfo("声音", "保存失败: "+err.Error())
				return
			}
			loc, _ := c.Location()
			sched.UpdateConfig(c, loc)
			logger.Printf("sound enabled=%v", enabled)
		}
	})
	m.AddCheckbox("开机自启", sched.CurrentConfig().Autostart, func() {
		cur := sched.CurrentConfig()
		enabled := !cur.Autostart
		if err := setAutostart(enabled); err != nil {
			showInfo("开机自启", "设置失败: "+err.Error())
			return
		}
		if c, err := config.Load(cfgPath); err == nil {
			c.Autostart = enabled
			if err := config.Save(cfgPath, c); err != nil {
				showInfo("开机自启", "保存失败: "+err.Error())
				return
			}
			loc, _ := c.Location()
			sched.UpdateConfig(c, loc)
		}
		logger.Printf("autostart=%v", enabled)
	})
	m.Add("重新加载配置", func() {
		if err := reloadConfig(cfgPath, sched, logger); err != nil {
			logger.Printf("reload failed: %v", err)
			showError("重新加载失败", err.Error())
		} else {
			tray.ShowNotification("PomodoroNotifier", "配置已重新加载")
		}
	})
	m.Add("打开配置目录", func() { openInExplorer(filepath.Dir(cfgPath)) })
	m.AddSeparator()
	m.Add("关于", func() { showAbout() })
	m.Add("退出", func() { tray.Remove(); tray.Hide(); os.Exit(0) })
	return m
}

// openSettings 打开设置窗口；保存成功后通过 onSaved 做附带副作用（如同步开机自启）。
func openSettings(cfgPath string, sched *scheduler.ServiceScheduler, logger *logging.Logger) {
	ui.ShowSettings(cfgPath, sched, logger, func(cfg config.AppConfig) {
		if err := setAutostart(cfg.Autostart); err != nil {
			showInfo("开机自启", "设置失败: "+err.Error())
		}
	})
}

// startUIDispatcher 在独立 goroutine 锁定 OS 线程，按顺序消费 popupQueue。
// webview2 库的 NewWithOptions/Run 强烈依赖"调用者线程有消息循环 + 在同一 OS 线程"。
// 在这里我们锁定 OS 线程后，库内部 goroutine 会自动绑定到同一线程，避开主线程
// 已被 systray.Run 占用的冲突。
func startUIDispatcher(logger *logging.Logger, sched *scheduler.ServiceScheduler, quoteTimeout time.Duration) {
	go func() {
		runtime.LockOSThread()
		for job := range popupQueue {
			logger.Printf("[ui] ShowPopup begin kind=%s", job.e.Kind)
			// 诗词在 dispatcher 协程里取，不阻塞调度 tick 协程。
			q := fetchQuote(sched.CurrentConfig(), quoteTimeout, logger)
			logger.Printf("[ui] quote: %q", q.Text)
			err := ui.ShowPopup(job.e, q, job.opts)
			if err != nil {
				logger.Printf("[ui] ShowPopup failed: %v", err)
				// 兜底：弹窗渲染失败时，用系统消息框展示提醒内容，
				// 避免提醒静默丢失；仅首次失败时提示，避免消息框刷屏。
				if webviewWarned.CompareAndSwap(false, true) {
					showInfo(job.e.Title, job.e.Message)
					showInfo("提醒组件异常", "弹窗渲染失败，已用系统消息框兜底提醒。\n可能缺少 WebView2 Runtime，请安装后重启。")
				}
			} else {
				logger.Printf("[ui] ShowPopup OK")
			}
		}
	}()
}

func emitManual(sched *scheduler.ServiceScheduler) {
	e := scheduler.PopupEvent{
		Kind:    "manual",
		Title:   "手动提醒",
		Message: "这是你点击托盘触发的提醒。",
		At:      time.Now(),
	}
	cur := sched.CurrentConfig()
	loc, _ := cur.Location()
	// 同样走 UI dispatcher，避免 systray 菜单回调线程触发 webview2 异常。
	// 非阻塞发送：队列满时丢弃（极少发生），不会冻结托盘菜单。
	job := popupJob{
		e: e,
		opts: ui.Options{
			Width:            cur.Popup.Width,
			Height:           cur.Popup.Height,
			AutoCloseSeconds: cur.Popup.AutoCloseSeconds,
			TopMost:          cur.Popup.TopMost,
			Position:         cur.Popup.Position,
			Loc:              loc,
			SoundEnabled:     cur.Popup.Sound.Enabled,
			SoundFile:        cur.Popup.Sound.File,
		},
	}
	select {
	case popupQueue <- job:
		// enqueued
	default:
		// 队列满，丢弃
	}
}

func reloadConfig(path string, sched *scheduler.ServiceScheduler, logger *logging.Logger) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	loc, err := cfg.Location()
	if err != nil {
		loc = time.Local
	}
	sched.UpdateConfig(cfg, loc)
	logger.Printf("config reloaded from %s", path)
	return nil
}

func watchConfig(path string, sched *scheduler.ServiceScheduler, logger *logging.Logger) {
	last, _ := os.Stat(path)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		if last == nil || stat.ModTime().After(last.ModTime()) {
			last = stat
			if err := reloadConfig(path, sched, logger); err != nil {
				logger.Printf("reload failed: %v", err)
			}
		}
	}
}

func fetchQuote(cfg config.AppConfig, timeout time.Duration, logger *logging.Logger) quote.Quote {
	url := cfg.QuoteAPI.URL
	if url == "" {
		return quote.Fallback()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	q, err := quote.Fetch(ctx, url)
	if err != nil {
		logger.Printf("fetch quote failed: %v", err)
		return quote.Fallback()
	}
	if strings.TrimSpace(q.Text) == "" {
		return quote.Fallback()
	}
	return q
}

func fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	showError("PomodoroNotifier 启动失败", msg)
	os.Exit(1)
}
