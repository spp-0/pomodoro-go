# PomodoroNotifier - 架构文档

> 面向"接手这个项目的下一个 agent / 工程师"。读完本篇应能：
> - 知道这个程序由哪些模块组成、各自负责什么
> - 知道一条弹窗事件从"到点"到"用户看到"经过的所有环节
> - 知道哪些坑是历史上踩过、被显式编码规避的
> - 知道哪些代码不能乱改（共享锁、webview2 线程约束等）

---

## 1. 项目目标

把 `c:\Users\Administrator\scripts\pomodoro.ps1`（番茄钟 + 气泡提醒脚本）改造成：
- **单进程 Windows GUI 程序**（`PomodoroNotifier.exe`）
- **托盘常驻**（不抢窗口、不弹控制台）
- **同时支持两种提醒**：
  1. 番茄钟循环（工作 N 分钟 / 休息 M 分钟）
  2. 指定时间点列表（每天 HH:MM 各弹一次；**每个时间点可单独配 title/message**，旧 `[]string` 格式自动兼容）
- **弹窗精美**：WebView2 渲染 HTML/CSS，渐变背景 + 卡片 + 倒计时 + ESC/Enter 关闭
- **随机诗词/名言**：在线接口 `v1.hitokoto.cn` + 内置 14 句中文兜底
- **配置热重载**：改 `config.json` 不需要重启；配置解析失败**降级为默认配置 + 弹窗提示**，绝不直接退出
- **图形化设置窗口**：右键托盘 → 设置…，WebView2 渲染的设置页直接改番茄钟 / 时间点 / 声音 / 弹窗位置 / 诗词 API / 开机自启，原子保存并热重载
- **提醒音**：可按 `popup.sound` 用系统默认提示音或自定义 wav
- **开机自启**：托盘开关写入注册表 `HKCU\...\Run`（无新依赖）
- **WebView2 兜底**：弹窗渲染失败时回退到系统消息框，提醒内容不丢失

---

## 2. 目录结构

```
pomodoro-go/
├── cmd/
│   ├── pomodoro-agent/                # 主程序：托盘 + 调度 + 弹窗调度
│   │   ├── main.go                    # main、托盘、UI dispatcher、emit、热重载
│   │   ├── winutil.go                 # Windows 工具：explorer、MessageBoxW
│   │   └── assets/
│   │       ├── tray.png               # 🔴 红色：专注中（与 tray_work.png 等价）
│   │       ├── tray_work.png          # 🔴 红色番茄：专注
│   │       ├── tray_break.png         # 🟡 黄色番茄：休息
│   │       └── tray_pause.png         # ⚪ 灰色番茄：暂停/待命
│   └── gentray/                       # 一次性工具：重新生成托盘 PNG
│       └── main.go
├── internal/
│   ├── config/                        # 配置加载/保存/默认值
│   │   └── config.go
│   ├── logging/                       # 简单文件日志（无依赖）
│   │   └── logging.go
│   ├── quote/                         # 在线 + 离线兜底语句
│   │   └── quote.go
│   ├── scheduler/                     # 调度器核心（线程安全）
│   │   ├── scheduler.go
│   │   └── scheduler_test.go          # 单测：去重/番茄钟相位/时区/热重载保留/旧格式兼容
│   └── ui/                            # WebView2 弹窗 + 设置窗口
│       ├── popup.go                   # ShowPopup + 声音 + 兜底消息框
│       └── settings.go                # 图形化设置窗口（WebView2 渲染）
├── go.mod
├── go.sum
├── dist/
│   ├── PomodoroNotifier.exe           # 编译产物
│   ├── config.json                    # 运行时配置（exe 同目录，首次运行自动生成）
│   └── pomodoro.log                   # 运行日志（同目录）
├── README.md                          # 用户文档（面向终端用户）
├── ARCHITECTURE.md                    # 本文件
├── API.md                             # 各包公开 API 速查
└── DEVELOPING.md                      # 开发指南：增删改查
```

---

## 3. 模块依赖图

```
              ┌────────────────────────────┐
              │  cmd/pomodoro-agent/main   │
              │  (main goroutine)          │
              └─────┬──────┬──────┬─────┬──┘
                    │      │      │     │
            ┌───────▼┐ ┌───▼───┐ ┌▼────┐ ┌▼────────────┐
            │config  │ │scheduler│ │quote│ │ui (popup)   │
            │        │ │        │ │     │ │ + webview2  │
            └────┬───┘ └────┬───┘ └──┬──┘ └──────┬──────┘
                 │          │        │            │
            ┌────▼────┐ ┌───▼──────┐ │       ┌────▼────────┐
            │logging  │ │  (无外部) │ │       │Krakinsight/ │
            │         │ │          │ │       │  go-webview2│
            └────┬────┘ └──────────┘ │       └─────────────┘
                 │                   │
            ┌────▼────────────────┐  │
            │  os.File  (no log   │  │
            │  rotation)          │  │
            └─────────────────────┘  │
                                  ┌──▼────────────┐
                                  │v1.hitokoto.cn │
                                  │(在线 quote)   │
                                  └───────────────┘
```

**外部依赖**：
| 包 | 用途 | 备注 |
|---|---|---|
| `github.com/gogpu/systray` | 系统托盘 | 零 CGO，纯 Go |
| `github.com/Krakinsight/go-webview2` | WebView2 弹窗 | 需要 Windows + WebView2 Runtime |

**为什么不引入 ORM/配置库/日志库**：
- 配置就是 JSON，不用 viper
- 日志就是文件追加，不用 zap/logrus
- 弹窗就是 HTML+JS 字符串，不用前端框架
- 减少依赖 = 减少未来维护负担 = 编译产物 ~10MB

---

## 4. 数据流：一次"到点"事件

```
        ┌──────────────────────────────────────┐
        │ main 协程启动 1 秒 ticker            │
        │  for now := range ticker.C {         │
        │      sched.Tick(now)                 │
        │  }                                    │
        └──────────────────┬───────────────────┘
                           │ 每秒一次
                           ▼
        ┌──────────────────────────────────────┐
        │ scheduler.Tick(now)                  │
        │   ├── tickTimepoints(cfg, now, emit) │ ──┐
        │   └── tickPomodoro(cfg, now, emit)   │   │ 都在锁内收集
        └──────────────────┬───────────────────┘   │ PopupEvent 列表
                           │                        │ 锁外 for emit
                           ▼                        │
        ┌──────────────────────────────────────┐   │
        │ emit(e scheduler.PopupEvent)        │ ◄─┘
        │   1) 检查 paused 原子标志            │
        │   2) cur := sched.CurrentConfig()    │ 取最新 cfg
        │   3) popupQueue <- popupJob{...}     │ 投到 UI 队列（**不含 quote**）
        └──────────────────┬───────────────────┘
                           │ buffered chan (cap 16)
                           ▼
        ┌──────────────────────────────────────┐
        │ startUIDispatcher goroutine          │
        │   runtime.LockOSThread()             │ 锁住 OS 线程
        │   for job := range popupQueue {      │ 同步阻塞消费
        │       q := fetchQuote(cur, ...)      │ 在线 quote（**在 dispatcher 取，不阻塞 tick**）
        │       ui.ShowPopup(job.e, q,        │ ← webview2 在这里
        │                  job.opts)           │
        │   }                                   │
        └──────────────────┬───────────────────┘
                           │ ShowPopup 阻塞（20s auto-close 或用户点"知道了"）
                           ▼
        ┌──────────────────────────────────────┐
        │ ui.ShowPopup                         │
        │   1) webview2.NewWithOptions(...)    │ 创建窗口
        │   2) w.Bind("closeWindow", ...)      │ 注册 JS 回调
        │   3) w.SetHtml(html)                 │ 注入 HTML
        │   4) w.Run()                         │ 阻塞直到关闭
        │   5) w.Destroy()                     │ 清理
        └──────────────────┬───────────────────┘
                           │ ShowPopup return
                           ▼
                       用户看到弹窗
```

---

## 5. 线程模型

| 角色 | 线程 / 协程 | 阻塞行为 | 关键约束 |
|---|---|---|---|
| **main goroutine** | 主线程（systray.Run） | 阻塞托盘消息循环 | **不能在这线程调 webview2** |
| **tick goroutine** | `time.Ticker(1s)` 所在协程 | 短（emit 内做轻工作） | 调 `sched.Tick` |
| **UI dispatcher** | `runtime.LockOSThread()` 独立 OS 线程 | 阻塞 ShowPopup（最长 auto_close 秒） | **唯一可以调 `ui.ShowPopup` 的地方** |
| **watch goroutine** | `time.Ticker(2s)` | 极短 | `os.Stat` 文件 mtime，调 `reloadConfig` |
| **UI dispatcher 内部 fetchQuote** | 同步 `http.Get`（仅 dispatcher 线程） | 最多 1500ms | **不再阻塞 tick**；失败一律 `quote.Fallback()` |

**为什么 UI dispatcher 要 LockOSThread**：
`Krakinsight/go-webview2` 的 `NewWithOptions` 内部要起一个绑定到调用者线程的子 goroutine；如果主线程已经被 `systray.Run` 占用，库内部会失败或 hang。我们让 UI dispatcher 锁住自己的 OS 线程，让 webview2 子 goroutine 钉到这条线程上。

**为什么不能在 emit 里直接 `go ui.ShowPopup`**：
1. emit 在 tick goroutine 上跑，没有自己的消息循环
2. webview2 库隐式要求调用线程有 OS 消息循环
3. 之前用 `go func() { ui.ShowPopup(...) }()` 直接**进程崩溃/卡住**（修这个 bug 的过程见 commit 历史）

**为什么不能把 emit 调度的 `PopupEvent` 收集和 emit 都放在锁内**：
`sync.Mutex` 不可重入。如果锁内调 emit、emit 又调 `sched.CurrentConfig()` → `mu.Lock()` → **永久死锁，goroutine hang，日志永远不出**（这是另一个修过的 bug）。

---

## 6. 关键不变式 (Invariants)

接手时**不要破坏**以下约定，否则会出现非常诡异的行为：

1. **调度器的 `mu` 持有期间不能调任何会回到 `Scheduler` 的方法**（`CurrentConfig` / `State` / `SetEmitter` / `UpdateConfig` / `Pause` / `SetStateListener`）。会死锁。
2. **`ui.ShowPopup` 必须在锁定 OS 线程的 goroutine 里调用**。直接放 main 或 tick 会出问题。
3. **emit 闭包必须从 `sched.CurrentConfig()` 取配置**，不能捕获启动时的 `cfg` 副本，否则配置热重载失效。
4. **`Scheduler.UpdateConfig` 会清空当天 `lastT` 标记**，但**仅在 `cfg.Pomodoro.Enabled == false` 时才清空番茄钟计时状态** —— 改时间点/文案/弹窗设置时不会打断进行中的番茄钟。如需"永久"标记已触发时间点，不要用 UpdateConfig 走 reload。
5. **托盘图标的 state 由 `applyTrayState` 统一管理**。不要在别处调 `tray.SetIcon`，否则会和 scheduler 的 `SetStateListener` 互相覆盖。
6. **`popupQueue` 容量 16**。如果短期内产生 >16 个事件，会被丢弃并打日志（`[emit] popup queue full, dropping event %s`）。弹窗是 UX 优先、不能堆队列。
7. **quote 拉取**：`cfg.QuoteAPI.Timeout` 默认 1500ms。超时或失败一律用 `quote.Fallback()` 兜底，弹窗永远有内容。
8. **配置解析失败必须降级**：`config.Load` 除"文件不存在"外，任何解析错误都**不再 `os.Exit`**，而是回退默认配置 + 通过 `showError` 弹窗提示，保证进程始终起得来。
9. **WebView2 渲染失败必须兜底**：`ui.ShowPopup` 返回 error 时，调用方先用 `tray.ShowNotification` 兜底，再用 `showError` 一次性系统消息框展示提醒内容，避免提醒静默丢失。
10. **设置窗口保存是原子的**：`ui.SaveConfig` 先写临时文件再 rename，避免写一半被读到。回调里改完配置必须调 `sched.UpdateConfig(c, loc)` 才生效。

---

## 7. 启动流程（main.go）

```
main()
  ├─ 解析 -config / -test 参数
  ├─ resolveConfigPath → config.DefaultConfigPath (exe 同目录)
  ├─ config.Load(path)
  │    ├─ 文件不存在 → 用 config.DefaultConfig() + config.Save(path, cfg) 生成
  │    ├─ 读取 + ApplyDefaults
  │    └─ 解析错误 → **降级默认配置 + 弹窗告警**（不退出）
  ├─ logger := logging.New(cfg.LogFile)
  ├─ if testMode: ui.ShowPopup(...) + return
  ├─ loc, err := cfg.Location()       // timezone（报错降级 time.Local）
  ├─ quoteTimeout, err := cfg.QuoteTimeout()
  ├─ sched := scheduler.New(cfg, loc, nil)   // emit 稍后注入
  ├─ scheduler.TickDebugf = logger.Printf("[tick] "...)
  ├─ 定义 emit 闭包
  ├─ sched.SetEmitter(emit)           // 解决初始化循环依赖
  ├─ go watchConfig(path, sched, logger)     // 2s 一次 mtime 检查
  ├─ startUIDispatcher(logger)        // LockOSThread 消费 popupQueue（含 fetchQuote）
  ├─ go tick goroutine                // 每秒 sched.Tick
  ├─ buildMenu(...)                   // 含 设置… / 声音 / 开机自启 开关
  └─ tray.Run() 阻塞                  // 主线程跑 systray
       └─ 菜单 / 状态切换 / 退出
       └─ 设置窗口（ui.OpenSettings）原子保存后调 sched.UpdateConfig 热重载
```

---

## 8. 关键历史 Bug 与修复（避免重蹈覆辙）

### Bug 1: `terminate()` 让宿主进程退出
**现象**：点"知道了"关弹窗 → 整个程序退出。
**原因**：Krakinsight 库 `w.Terminate()` 强行结束宿主进程。
**修复**：用 `w.Destroy()` 替代，2s 兜底。

### Bug 2: 配置热重载不生效
**现象**：改 `popup.position` 后弹窗还在老位置。
**原因**：emit 闭包捕获了启动时的 `cfg` 副本；`sched.UpdateConfig` 只更新 scheduler 内部 cfg。
**修复**：emit 改用 `cur := sched.CurrentConfig()` 每次取最新。

### Bug 3: 时间点列表不弹窗
**现象**：17:18 触发，scheduler 提示 FIRE，但 emit 永远不到 ShowPopup。
**原因**：`tickTimepoints` 在 `s.mu.Lock() ... defer s.mu.Unlock()` 期间调 emit，emit 内部又调 `sched.CurrentConfig()` → `mu.Lock()` → **死锁**。
**修复**：tickTimepoints / tickPomodoro 改为**锁内只收集事件列表，锁外 for emit**。

### Bug 4: timepoint 当天 17:18 已过、reload 配置后不立即触发
**现象**：配置变更后，新增的"当前分钟 = HH:MM" 不弹。
**修复**：`UpdateConfig` 加 `fireImmediateTimepointsLocked`：如果当前分钟匹配，立即补一次。

### Bug 5: bottom-left / bottom-right 不生效
**现象**：用 webview2 库的负坐标 `Location{X: -W-16, Y: -H-16}` 解释为"屏幕外"。
**原因**：该库对负值语义与文档不符。
**修复**：改用 `user32!SystemParametersInfoW(SPI_GETWORKAREA)` 拿绝对像素坐标。

### Bug 6: 弹窗在 tick goroutine 跑会卡住
**现象**：手动"立即弹一次"（走 popupQueue）成功，timepoint 弹窗卡死。
**原因**：timepoint 走 emit → emit 同步调 ui.ShowPopup → ShowPopup 在 tick goroutine 跑 webview2 → 线程模型不匹配。
**修复**：所有 ShowPopup 走 UI dispatcher 队列；UI dispatcher 用 `runtime.LockOSThread()` 锁定 OS 线程。

### Bug 7: `once_per_day=false` 时一分钟内刷屏 ~60 次
**现象**：关掉"每天一次"后，命中的那一分钟里每秒弹一次窗。
**原因**：去重逻辑被 `once_per_day` 开关错误耦合——只有该开关为 true 时才写"已触发"标记，false 时每分钟每秒都重新 FIRE。
**修复**：`tickTimepoints` 去重改为**始终**基于"天+分钟"标记，彻底移除 `once_per_day` 字段。`timepoint.times` 改用对象数组（自定义 `UnmarshalJSON` 兼容旧 `[]string`）。

### Bug 8: config.json 写错一个逗号进程直接退出
**现象**：用户手改配置漏了逗号，工具启动即 `os.Exit(1)`，托盘都不出现。
**原因**：`main` 里 `config.Load` 除"文件不存在"外的任何错误都直接 `os.Exit`。提醒工具"起不来"比"弹错"更糟。
**修复**：解析错误降级为默认配置 + `showError` 弹窗提示，进程始终启动；配置仍会被 `watchConfig` 每 2s 重试。

### Bug 9: WebView2 缺失/失败时提醒静默丢失
**现象**：目标机没装 WebView2 Runtime 或窗口创建失败，事件被 `log` 后丢弃，用户完全没收到提醒。
**原因**：`ShowPopup` 返回 error 后仅打日志，没有兜底通道。
**修复**：调用方先用 `tray.ShowNotification` 兜底，再用 `showError` 一次性系统消息框展示标题/内容，确保提醒内容不丢。

### Bug 10: 取诗词同步阻塞每秒调度
**现象**：断网/慢网时 `emit` 内 `fetchQuote` 最长卡 1500ms，调度 tick 被拖慢，弹窗延迟。
**原因**：`fetchQuote` 放在 tick 协程的 emit 闭包里同步执行。
**修复**：诗词移到 UI dispatcher 协程里取（消费 popupQueue 时），与每秒 tick 解耦；`quote.go` 移除冗余 `http.Client.Timeout`，由 `context` 兜底。

---

## 9. 怎么读这份代码

如果你第一次来这个项目，建议按这个顺序：

1. `cmd/pomodoro-agent/main.go` — 看 100~180 行（main 函数）了解启动顺序
2. `internal/scheduler/scheduler.go` — 看 `Tick` / `tickPomodoro` / `tickTimepoints` 了解调度
3. `internal/ui/popup.go` — 看 `ShowPopup` 了解 webview2 集成 + `computeLocation` 了解位置算法
4. `internal/config/config.go` — 看 `AppConfig` 结构与默认值
5. `internal/quote/quote.go` — 极简
6. `internal/logging/logging.go` — 极简

---

## 10. 后续工作（M3 及以后）

以下为尚未实现、设计上预留扩展点的功能（详见 [DEVELOPING.md](./DEVELOPING.md)）；**已完成的 M1+M2 项不再列出**：

- **snooze 按钮**：弹窗里"5/10/15 分钟后再提醒"
- **多显示器支持**：用所在屏幕的 work area 计算位置（现为单显示器 work area）
- **统计面板**：每天完成多少番茄钟、时间点命中次数
- **不抢焦点 / 非置顶模式**：`popup.topmost=false` 时完全不打断当前窗口
- **依赖瘦身**：当前 `go-webview2` 的小众 fork 拉入了 webgpu/cbor 等重传递依赖（二进制 ~14MB），可评估切回官方 `webview/webview2` 或自维护最小绑定
- 多账号 / 多配置文件

> 历史诊断与修复清单见 [PRODUCT_REVIEW.md](./PRODUCT_REVIEW.md)。
