# PomodoroNotifier - 公开 API 速查

> 给 agent 用的"包 API + 调用约定"速查。
> 每个 API 都标注：**作用 / 签名 / 线程安全 / 调用约束 / 何时该用**。

---

## 1. `internal/config`

配置文件路径解析、JSON 加载/保存、默认值填充。

### 1.1 类型

```go
type AppConfig struct {
    LogFile    string           `json:"log_file"`
    TimeZone   string           `json:"timezone"`
    QuoteAPI   QuoteAPIConfig
    Popup      PopupConfig
    Pomodoro   PomodoroConfig
    Timepoint  TimepointConfig
}

type QuoteAPIConfig struct {
    URL     string `json:"url"`
    Timeout string `json:"timeout"`  // Go duration 字符串, 如 "1500ms", "3s"
}

type PopupConfig struct {
    AutoCloseSeconds int    `json:"auto_close_seconds"`
    Width            int    `json:"width"`
    Height           int    `json:"height"`
    ClickThrough     bool   `json:"click_through"`
    TopMost          bool   `json:"topmost"`
    Position         string `json:"position"`  // center / top-left / top-right / bottom-left / bottom-right
}

type PomodoroConfig struct {
    Enabled      bool   `json:"enabled"`
    WorkMinutes  int    `json:"work_minutes"`
    BreakMinutes int    `json:"break_minutes"`
    WorkDays     []int  `json:"work_days"`   // 1=周一 ... 7=周日
    WorkStart    string `json:"work_start"`  // "HH:MM"
    WorkEnd      string `json:"work_end"`
    WorkText     string `json:"work_text"`
    BreakText    string `json:"break_text"`
}

type TimepointConfig struct {
    Enabled    bool     `json:"enabled"`
    Times      []string `json:"times"`     // 每个元素 "HH:MM"
    Title      string   `json:"title"`
    Message    string   `json:"message"`
    OncePerDay bool     `json:"once_per_day"`
}
```

### 1.2 函数

#### `DefaultConfig() AppConfig`
- 完整默认值
- 不读文件；纯函数
- **用途**：首次启动生成 `config.json` 时调用

#### `Load(path string) (AppConfig, error)`
- 读 JSON
- 找不到文件 → 返回 `os.IsNotExist` 错误，**由 main 包决定要不要 `DefaultConfig + Save`**
- 读取后自动 `ApplyDefaults`（补全缺失字段）
- **错误**：JSON 解析错误 / 文件权限错误

#### `Save(path string, cfg AppConfig) error`
- 缩进 JSON 写入
- 自动 `os.MkdirAll` 父目录
- 写入前自动 `ApplyDefaults`
- **用途**：写默认配置、热重载时持久化

#### `(c *AppConfig) ApplyDefaults()`
- 补齐缺省值（`Position` 归一化、Timeout 兜底、Pomodoro 缺省工作日 1-5 等）
- **注意**：会修改入参（指针接收）

#### `ExeDir() (string, error)` / `DefaultConfigPath() (string, error)`
- `ExeDir`：当前可执行文件目录
- `DefaultConfigPath`：`<exe-dir>/config.json`
- **用途**：用户在命令行没传 `-config` 时

#### `(c AppConfig) Location() (*time.Location, error)`
- 解析 `cfg.TimeZone`（IANA 名，如 `"Asia/Shanghai"`）
- 空字符串返回 `time.Local`
- **错误**：未知时区

#### `(c AppConfig) QuoteTimeout() (time.Duration, error)`
- 解析 `cfg.QuoteAPI.Timeout`
- **用途**：把字符串 `"1500ms"` 转成 `time.Duration`

---

## 2. `internal/logging`

极简文件日志，**无依赖、无级别、无滚动**。

### 2.1 类型 / 函数

```go
type Logger struct { /* ... */ }

func New(path string) *Logger
func (l *Logger) Printf(format string, args ...any)
```

### 2.2 行为
- 启动时 `os.OpenFile(..., O_APPEND|O_CREATE|O_WRONLY, 0644)`
- 每次 `Printf` 加时间戳前缀
- 写错误**静默忽略**（不阻塞业务）
- **用法**：
  ```go
  logger := logging.New("pomodoro.log")
  logger.Printf("start config=%s", path)
  ```

### 2.3 注意
- 文件无锁：多协程并发写会交错，**当前只有 main goroutine + UI dispatcher 写，安全**
- 不会自动 rotate；如果日志巨大需要手动加

---

## 3. `internal/quote`

拉取在线诗词/名言 + 离线兜底。

### 3.1 类型

```go
type Quote struct {
    Text   string
    Author string
    Source string
}
```

### 3.2 函数

#### `Fetch(ctx context.Context, url string) (Quote, error)`
- 同步 HTTP GET
- 内部 hard-coded `client.Timeout = 5 * time.Second`（**注意**：外面应该用 ctx 控制更短 timeout）
- 默认期望 `v1.hitokoto.cn/?encode=json` 协议：返回 `{hitokoto, from, from_who}`
- **错误**：
  - empty url
  - HTTP 非 2xx
  - JSON 解析错误
  - 空 hitokoto
- **线程安全** ✅

#### `Fallback() Quote`
- 返回 14 句内置中文诗词/格言的随机一条
- 用 `math/rand`（不是 crypto/rand，因为不需要安全性）
- 包含：唐诗宋词 + 莎士比亚/乔布斯/海明威等
- **线程安全** ✅（rand.Intn 内部有锁）

---

## 4. `internal/scheduler`

调度器核心：**双源触发**（番茄钟循环 + 时间点列表）。

### 4.1 类型

```go
type PopupEvent struct {
    Kind    string    // pomodoro_break_start / pomodoro_break_end / timepoint / manual / test
    Title   string
    Message string
    At      time.Time
}

type Emitter func(PopupEvent)

type State string
const (
    StateIdle   State = "idle"
    StateWork   State = "work"
    StateBreak  State = "break"
)

type ServiceScheduler struct { /* 私有 */ }
```

### 4.2 构造与生命周期

#### `New(cfg config.AppConfig, loc *time.Location, emit Emitter) *ServiceScheduler`
- 创建调度器，初始状态 `StateIdle`
- `emit` 可传 nil，后续用 `SetEmitter` 注入
- `loc` 决定"HH:MM"按哪个时区解释

#### `SetEmitter(emit Emitter)`
- 替换 emit 回调
- 线程安全 ✅
- **用途**：避免 main 初始化时的循环依赖（sched 需先存在，emit 才能引用 sched）

#### `(s *ServiceScheduler) UpdateConfig(cfg config.AppConfig, loc *time.Location)`
- 替换配置 + 时区
- 清空当天 `lastT`（已触发时间点标记）
- 清空番茄钟状态
- **如果 cfg 中某 timepoint 命中"当前分钟"，立即补一次**（修过"改完配置立即验证"问题）
- **线程安全** ✅
- **调用约束**：不能在持锁状态下调用（废话，正常不会）

#### `(s *ServiceScheduler) Pause()`
- 清空番茄钟状态、清空时间点已触发标记
- 触发状态变更回调 `StateIdle`
- **线程安全** ✅
- **不会改 cfg**

#### `(s *ServiceScheduler) Tick(now time.Time)`
- 主调度入口，每秒调一次
- 内部按 `loc` 归一化
- 串行触发 `tickTimepoints` → `tickPomodoro`
- **线程安全** ✅（用 mu 保护所有状态）

#### `(s *ServiceScheduler) SetStateListener(fn func(State))`
- 注册状态变更回调
- 触发时机：进入 work / break / idle 状态
- **调用约束**：回调里**不要阻塞**（在锁内被调，阻塞会死锁）；不要回调中再调 sched 的方法

#### `(s *ServiceScheduler) State() State`
- 当前状态

#### `(s *ServiceScheduler) CurrentConfig() config.AppConfig`
- 返回当前生效配置（深拷贝按值语义）
- **emit 闭包必须用这个**，不能用闭包捕获的 cfg 副本

### 4.3 全局变量

#### `var TickDebugf = func(format string, args ...any) {}`
- 默认空（no-op）
- 由 main 包注入：`scheduler.TickDebugf = func(...) { logger.Printf("[tick] "...) }`
- 用于在每个 timepoint 关键节点打印日志

### 4.4 内部算法（了解即可，一般不改）

- **番茄钟**：`pomodoroState.phase` 在 work/break 切换；`nextAt` 决定下次切换时间；只在 `work_days` ∩ `work_start..work_end` 内运行；非工作时间自动 `StateIdle`
- **时间点**：`lastT[dayKey][minKey]` 记录当天是否已触发；`lastLog[dayKey|minKey]` 抑制"已触发"日志重复打印

### 4.5 怎么加新触发源

模板（伪代码）：

```go
// 1) 在 ServiceScheduler 加状态字段
type ServiceScheduler struct {
    // ...
    myState myStateType
}

// 2) 加 Tick 子方法
func (s *ServiceScheduler) tickMyThing(cfg config.AppConfig, now time.Time, emit Emitter) {
    if !cfg.MyThing.Enabled { return }
    var toFire []PopupEvent
    s.mu.Lock()
    // ... 收集 toFire ...
    s.mu.Unlock()
    for _, e := range toFire { emit(e) }  // 锁外 emit
}

// 3) 在 Tick 里调用
func (s *ServiceScheduler) Tick(now time.Time) {
    // ...
    s.tickTimepoints(cfg, now, emit)
    s.tickPomodoro(cfg, now, emit)
    s.tickMyThing(cfg, now, emit)  // 新增
}
```

⚠️ **关键**：emit 必须在锁外调用。emit 可能回到 `CurrentConfig()` 拿配置，那时会 `mu.Lock()`，**锁内 emit = 死锁**。

---

## 5. `internal/ui`

WebView2 弹窗。

### 5.1 类型

```go
type Options struct {
    Width            int
    Height           int
    AutoCloseSeconds int
    TopMost          bool
    Position         string  // center / top-left / top-right / bottom-left / bottom-right
}

type data struct { /* 私有，渲染到 HTML 用 */ }
```

### 5.2 函数

#### `ShowPopup(e scheduler.PopupEvent, q quote.Quote, opt Options) error`
- 弹一个 WebView2 窗口
- **同步阻塞**直到：
  - 用户点"知道了" → `closeWindow` JS 回调 → `w.Destroy()`
  - 倒计时归零 → JS 自动 `closeWindow`
  - 2s 兜底（用户没点、JS 失败时强制 `w.Destroy()`）
- 返回 `nil` 表示正常关闭
- **线程约束**：**必须在 `runtime.LockOSThread()` 锁住的 OS 线程上调用**。否则 webview2 库会失败。
- **WebView2 失败时**：返回 error，调用方应打日志但不要 panic
- **重复调用**：可以并发调（不同 OS 线程），但要保证每个调用者都 LockOSThread

### 5.3 HTML 模板 (`pageTemplate`)

一个完整的 HTML 文档，包含：
- CSS 渐变背景 + 卡片阴影
- 倒计时 JS（每秒 -1，归零时调 `window.closeWindow()`）
- 自动 ESC/Enter 关闭
- "知道了"按钮

通过 `{{.PayloadB64}}` 注入 base64 编码的 JSON 数据；JS 解码后用 DOM API 写回字段（防 HTML 转义问题）。

### 5.4 `computeLocation(opt Options) (*webview2.Location, bool)`
- 计算弹窗左上角像素坐标
- 用 `user32!SystemParametersInfoW(SPI_GETWORKAREA)` 拿主屏工作区
- 自动避开任务栏
- 4 角 + center
- `center` 时 `*Location` 返回 nil、第二个返回值 true（让 webview2 自己居中）

### 5.5 `getWorkArea() (winRect, bool)`
- 调 Windows API
- 失败时返回 `(zero, false)`，调用方应回退到左上角 16px

### 5.6 怎么改 UI

**改背景色 / 字体 / 布局**：编辑 `pageTemplate` 里的 CSS。

**加按钮（snooze 等）**：
1. 在 `pageTemplate` 加 `<button id="snooze">` 元素
2. 改 JS 让 snooze 调一个新的 `Bind` 回调（如 `window.snooze5`）
3. `ShowPopup` 里用 `w.Bind("snooze5", func() { ... })` 注册
4. snooze 行为（如"5 分钟后弹窗"）需要在 main 包的 UI dispatcher 里实现（重新投递一个新 popupJob）

⚠️ **不要在 ShowPopup 里直接再调 `s.ShowPopup`（嵌套弹窗）**：当前实现没有这种能力，需要新设计。

---

## 6. `cmd/pomodoro-agent/main` (main 包内部)

main 包**不是公开 API**，但有些"内部约定"新 agent 要知道。

### 6.1 关键函数

| 函数 | 行号参考 | 作用 |
|---|---|---|
| `main()` | 45-178 | 入口；启动顺序见 [ARCHITECTURE §7](./ARCHITECTURE.md#7-启动流程maingo) |
| `startUIDispatcher(logger)` | 246-259 | 启动 LockOSThread 的 goroutine 消费 popupQueue |
| `applyTrayState(tray, st, paused)` | 181-201 | 切换托盘图标 + tooltip |
| `buildMenu(tray, cfgPath, sched, logger, paused)` | 210-240 | 构建托盘菜单 |
| `emitManual(sched)` | 261-281 | 立即弹一次 |
| `reloadConfig(path, sched, logger)` | 283-295 | 读 JSON + UpdateConfig |
| `watchConfig(path, sched, logger)` | 297-313 | 2s 一次 mtime 检查 |
| `fetchQuote(cfg, timeout, logger)` | 315-331 | 拉 quote，失败 fallback |

### 6.2 全局变量

| 变量 | 作用 |
|---|---|
| `popupQueue chan popupJob` (cap 16) | UI dispatcher 输入队列 |
| `trayWorkPNG / trayBreakPNG / trayPausePNG []byte` | 3 张内嵌托盘 PNG |
| `var paused atomic.Bool` (main 局部) | 用户"暂停"标志 |

### 6.3 怎么加新托盘菜单项

在 `buildMenu` 里：

```go
m.Add("我的新功能", func() {
    // 注意：这里在 systray 菜单回调线程，**不要直接调 ui.ShowPopup**，
    // 走 popupQueue
    cur := sched.CurrentConfig()
    popupQueue <- popupJob{
        e: scheduler.PopupEvent{Kind: "my_kind", Title: "...", Message: "...", At: time.Now()},
        q: quote.Fallback(),
        opts: ui.Options{Width: cur.Popup.Width, Height: cur.Popup.Height, AutoCloseSeconds: 10, TopMost: cur.Popup.TopMost, Position: cur.Popup.Position},
    }
})
```

### 6.4 怎么改图标

1. 编辑 `cmd/gentray/main.go` 改主题色
2. 跑 `go run .\cmd\gentray` 重新生成 PNG
3. 重新 `go build -ldflags "-H windowsgui -s -w" -o .\dist\PomodoroNotifier.exe .\cmd\pomodoro-agent`
