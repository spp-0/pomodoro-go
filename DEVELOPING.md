# PomodoroNotifier - 开发指南

> 面向"接手这个项目的下一个 agent / 工程师"。本篇回答 4 类问题：
> 1. **改什么文件** —— 加新功能 / 修 bug / 改 UI / 改配置
> 2. **怎么改** —— 步骤、模板
> 3. **怎么测** —— 本地编译、运行、日志怎么看
> 4. **怎么排错** —— 已知坑、日志关键字

如果还不了解项目结构，先看 [ARCHITECTURE.md](./ARCHITECTURE.md)。
如果要找某个 API 的签名，看 [API.md](./API.md)。

---

## 1. 环境

- Go 1.25+（无 CGO，但依赖用 `go install` / `go get`）
- Windows 10+（WebView2 Runtime 预装）
- 工具链：`go build`, `go run`, `go mod tidy`

> 验证：进入 `c:\Users\Administrator\scripts\pomodoro-go` 跑 `go version`，输出应 ≥ `go1.25`。

---

## 2. 常用命令

```powershell
# 1) 拉依赖（首次 / 改了 go.mod）
cd c:\Users\Administrator\scripts\pomodoro-go
go mod tidy

# 2) 全量编译检查（不会生成 exe）
go build ./...

# 3) 生成 GUI 子系统 exe（无控制台窗口）
go build -ldflags "-H windowsgui -s -w" -o .\dist\PomodoroNotifier.exe .\cmd\pomodoro-agent

# 4) 弹一次测试窗口（生成默认 config.json + pomodoro.log 后弹窗）
.\dist\PomodoroNotifier.exe -test

# 5) 正常运行（托盘常驻）
.\dist\PomodoroNotifier.exe

# 6) 指定配置
.\dist\PomodoroNotifier.exe -config C:\path\to\config.json

# 7) 重新生成托盘图标 PNG
go run .\cmd\gentray
```

---

## 3. 常见变更模板

### 3.1 加新配置字段

场景：比如要加"提醒音开关" `sound_enabled`。

**步骤**：

1. **改 `internal/config/config.go`**：
   ```go
   type PopupConfig struct {
       // ... 已有字段
       SoundEnabled bool `json:"sound_enabled"`
   }
   ```

2. **在 `DefaultConfig()` 补默认值**：
   ```go
   SoundEnabled: true,
   ```

3. **在 `ApplyDefaults()` 补兜底**（可选，按需）：
   ```go
   // 如果你有特殊归一化逻辑（如 Position）
   ```

4. **使用方**（如 `internal/ui/popup.go` 或 main）：
   ```go
   cur := sched.CurrentConfig()
   if cur.Popup.SoundEnabled {
       // 播放声音
   }
   ```

5. **测试**：删 `dist\config.json`，重新运行让程序生成新 schema。

**注意**：JSON 字段名用 snake_case (`sound_enabled`)，Go 字段名用 PascalCase (`SoundEnabled`)。

---

### 3.2 加新托盘菜单项

场景：加"打开日志"菜单项。

**步骤**：编辑 `cmd/pomodoro-agent/main.go` 的 `buildMenu` 函数：

```go
func buildMenu(...) *systray.Menu {
    m := systray.NewMenu()
    m.Add("立即弹一次", func() { emitManual(sched) })
    m.AddSeparator()
    // ... 已有项 ...
    m.AddSeparator()
    // ↓↓↓ 新增 ↓↓↓
    m.Add("打开日志", func() {
        // 假设日志路径在 sched.CurrentConfig().LogFile
        cur := sched.CurrentConfig()
        logPath := filepath.Join(filepath.Dir(cfgPath), filepath.Base(cur.LogFile))
        openInExplorer(logPath)  // winutil.go 已有
    })
    m.AddSeparator()
    m.Add("关于", func() { showAbout() })
    m.Add("退出", func() { tray.Remove(); tray.Hide(); os.Exit(0) })
    return m
}
```

**注意**：
- 菜单回调是 systray 内部线程，**不要**在这里直接调 `ui.ShowPopup`，要 `popupQueue <- popupJob{...}`
- 不要忘了加 `m.AddSeparator()` 分隔

---

### 3.3 改弹窗 UI

场景：加一个"snooze 5 分钟"按钮。

**步骤**：

1. **改 `internal/ui/popup.go` 的 `pageTemplate`（HTML 模板）**：
   ```html
   <button class="btn" id="snooze5">5分钟后再提醒</button>
   ```

2. **在 `ShowPopup` 注册新的 JS 回调**：
   ```go
   snoozeCh := make(chan struct{}, 1)
   w.Bind("snooze5", func() {
       select {
       case snoozeCh <- struct{}{}:
       default:
       }
       // 同时关闭当前弹窗
       w.Destroy()
       select {
       case <-closed:
       default:
       close(closed)
       }
   })
   ```

3. **在 `ShowPopup` 返回前处理 snooze**：
   ```go
   w.Run()  // 阻塞直到 closeWindow 或 snooze5
   
   // 检查是否 snooze
   select {
   case <-snoozeCh:
       // 把一个"延迟 5 分钟"的弹窗任务投回队列
       // （需要在外面实现；ShowPopup 是阻塞的，无法自己投递）
   default:
   }
   ```

   ⚠️ **当前 ShowPopup 是阻塞函数**，要实现 snooze 比较麻烦，需要在 main 包的 UI dispatcher 里包一层：
   - 让 ShowPopup 接受一个 `onSnooze func()` 回调
   - 触发 snooze 时调 `onSnooze`
   - `onSnooze` 由调用方实现：从 `popupQueue` 投一个"延迟 5 分钟"的 job

4. **修改 main 的 `popupJob`** 加 `onSnooze` 字段，`emitManual` 之类的地方提供实现。

---

### 3.4 加新触发源（如"每周一三五的 9:00 提醒"）

**步骤**：

1. **改 `internal/config/config.go`**：
   ```go
   type WeeklyConfig struct {
       Enabled bool       `json:"enabled"`
       Items   []WeeklyItem `json:"items"`
   }
   type WeeklyItem struct {
       Weekdays []int  `json:"weekdays"`  // 1=周一 ... 7=周日
       Time     string `json:"time"`      // "HH:MM"
       Title    string `json:"title"`
       Message  string `json:"message"`
   }
   type AppConfig struct {
       // ...
       Weekly WeeklyConfig
   }
   ```

2. **改 `internal/scheduler/scheduler.go`**：
   - 加 `ServiceScheduler.lastWeekly map[dayKey]map[itemID]bool` 跟踪已触发
   - 加 `Tick` 里的 `s.tickWeekly(cfg, now, emit)`
   - 实现 `tickWeekly`（参考 `tickTimepoints` 模板）

   ⚠️ **遵循锁模式**：锁内收集 toFire，锁外 for emit。

3. **改 `cmd/pomodoro-agent/main.go`**：
   - 不需要改 main（emit 是统一入口）

4. **测试**：加一个"今天"匹配的配置项，观察日志 + 弹窗。

---

### 3.5 改托盘图标

**步骤**：

1. **改 `cmd/gentray/main.go` 的 `themes` 字典**：
   ```go
   var themes = map[string]palette{
       "work":  {body: [3]uint8{232, 70, 70}, ...},
       // ...
   }
   ```

2. **重新生成**：
   ```powershell
   go run .\cmd\gentray
   ```

3. **重新编译**：
   ```powershell
   go build -ldflags "-H windowsgui -s -w" -o .\dist\PomodoroNotifier.exe .\cmd\pomodoro-agent
   ```

⚠️ 重新编译时**先把旧进程杀掉**（`Get-Process -Name PomodoroNotifier | Stop-Process`），否则 `dist\PomodoroNotifier.exe` 会被占用。

---

### 3.6 修 bug 通用流程

1. **复现**：跑 `-test` 或正常运行，确认问题
2. **看日志**：`dist\pomodoro.log` 找关键字：
   - `[tick] ...` — 调度器内部
   - `[emit] ...` — emit 闭包
   - `[ui] ...` — UI dispatcher
3. **定位模块**：根据日志关键字判断在哪个环节
4. **改代码 + 加日志**（如果是关键路径，先临时加 `logger.Printf`，验证完删）
5. **重新编译 + 复测**

---

## 4. 调试技巧

### 4.1 看实时日志

PowerShell：
```powershell
Get-Content -Path .\dist\pomodoro.log -Wait
```

（**注意**：GUI 子系统下没有控制台，printf 到 stderr 不显示，必须看文件。）

### 4.2 临时打开控制台输出

调试时如果想直接在控制台看日志，把 `-H windowsgui` 去掉：
```powershell
go build -ldflags "-s -w" -o .\dist-debug\PomodoroNotifier.exe .\cmd\pomodoro-agent
```

或者用 `logger.Printf` + `os.Stdout`（需要临时改 logging.go）。

### 4.3 模拟"程序启动 N 秒后发生了什么"

最简单的方法：临时把 main 的 ticker 间隔改小：

```go
ticker := time.NewTicker(200 * time.Millisecond)  // 改 1*time.Second
```

⚠️ 改完记得改回去，不然每秒 5 次 Tick。

### 4.4 隔离测试 scheduler

写个 `cmd/test-scheduler/main.go`：

```go
package main

import (
    "fmt"
    "time"
    "pomodoro-notifier/internal/config"
    "pomodoro-notifier/internal/scheduler"
)

func main() {
    cfg := config.DefaultConfig()
    sched := scheduler.New(cfg, time.Local, func(e scheduler.PopupEvent) {
        fmt.Printf("event: %+v\n", e)
    })
    for i := 0; i < 5; i++ {
        sched.Tick(time.Now())
        time.Sleep(200 * time.Millisecond)
    }
}
```

这样能纯函数式测试调度器逻辑（不需要托盘、webview2）。

### 4.5 隔离测试 quote

类似，写个 `cmd/test-quote/main.go`：

```go
package main

import (
    "context"
    "fmt"
    "time"
    "pomodoro-notifier/internal/quote"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    q, err := quote.Fetch(ctx, "https://v1.hitokoto.cn/?encode=json")
    fmt.Printf("quote: %+v err: %v\n", q, err)
}
```

---

## 5. 已知坑（避坑清单）

### 5.1 编译被占用
**症状**：`go build` 报 `cannot remove ... file is in use`
**解决**：
```powershell
Get-Process -Name PomodoroNotifier | Stop-Process
```
**或** 换一个输出目录（不要覆盖正在运行的 exe）。

### 5.2 WebView2 弹窗不显示
**症状**：日志有 `[ui] ShowPopup begin`，但看不到窗口。
**排查**：
- 检查 `dist\pomodoro.log` 有没有 `[ui] ShowPopup OK`
- 看 `popup.position` 配置（4 角的要算工作区）
- 确认 WebView2 Runtime 已装：`Get-ItemProperty -Path "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"`

### 5.3 改了配置不生效
**症状**：改 `config.json` 后 30 秒还没生效。
**原因**：watchConfig 每 2s 检查 mtime；如果是远程盘 / 一些杀毒软件，mtime 可能更新延迟。
**解决**：右键托盘 → "重新加载配置"。

### 5.4 时区错乱
**症状**：到点不弹 / 提前/延后弹。
**原因**：`cfg.Timezone` 设置了不同时区，或 Windows 时区被改。
**解决**：
- 删 `config.json` 让程序重新生成（默认用 `time.Local` = Windows 时区）
- 或显式设 `"timezone": ""` 用本地时区

### 5.5 中文显示乱码
**症状**：弹窗里诗词显示 ???
**原因**：一般是命令行/控制台编码问题，但我们的弹窗是 HTML（UTF-8）不会出问题。
**解决**：如果真出现，**重新编译**（编译时加 `-ldflags "-H windowsgui -s -w"`），不要用控制台模式跑。

### 5.6 死锁（最严重）
**症状**：日志停在 `[emit] called` / `[emit] popup: pos=...` 等某一行后面。
**原因**：emit 链路上有人在持 `Scheduler.mu` 时调 `CurrentConfig()` / `State()` 等。
**自检 checklist**：
- [ ] 改 scheduler 时，所有 emit 调用都在锁外？
- [ ] 改 main 时，所有 emit 闭包都用 `sched.CurrentConfig()` 而不是闭包捕获的 cfg？
- [ ] 加新的回调时，没有在锁内同步调用户的函数？

---

## 6. 发布 checklist

发布新版本前：

1. `go mod tidy` 拉齐依赖
2. `go build ./...` 全部包能编译
3. `go vet ./...` 没警告
4. 删除 `dist\config.json` 和 `dist\pomodoro.log`
5. 编译 GUI 版本
6. 跑 `-test` 验证弹窗
7. 正常运行 5 分钟，验证：
   - 托盘图标显示
   - 立即弹一次菜单
   - 暂停/恢复菜单
   - 重新加载配置菜单
   - 打开配置目录菜单
   - 退出菜单
8. 改 `dist\config.json` 触发 reload，验证配置生效
9. 把 `dist\PomodoroNotifier.exe` 单独拷给用户
   - 用户的 `config.json` / `pomodoro.log` 不要覆盖

---

## 7. 维护者快速参考

| 想做 | 改的文件 | 关键 API |
|---|---|---|
| 改默认值 | `internal/config/config.go` | `DefaultConfig()` |
| 加新配置项 | `internal/config/config.go` + 用到该字段的所有文件 | `AppConfig` 字段 |
| 改调度逻辑 | `internal/scheduler/scheduler.go` | `tickPomodoro` / `tickTimepoints` |
| 加新调度源 | `internal/scheduler/scheduler.go` | 新 `tickXxx` 方法 + 在 `Tick` 里调 |
| 改弹窗样式 | `internal/ui/popup.go` | `pageTemplate` |
| 改弹窗位置算法 | `internal/ui/popup.go` | `computeLocation` |
| 加新托盘菜单 | `cmd/pomodoro-agent/main.go` | `buildMenu` |
| 改托盘图标 | `cmd/gentray/main.go` | `themes` 字典 |
| 改 UI 线程策略 | `cmd/pomodoro-agent/main.go` | `startUIDispatcher` |
| 改日志格式 | `internal/logging/logging.go` | `Printf` |

---

## 8. 常见错误信息

| 错误 | 原因 | 解决 |
|---|---|---|
| `cannot remove dist/PomodoroNotifier.exe: file is in use` | 程序在跑 | `Get-Process \| Stop-Process` 后再 build |
| `WebView2 runtime not found` | 旧版 Windows 缺组件 | 装 [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) |
| `systray: failed to create icon` | PNG 损坏 / 路径错 | `go run .\cmd\gentray` 重新生成 |
| 日志没新行 | GUI 子系统下 stderr 不显示 | 看 `pomodoro.log` |
| 配置改完不弹 | lastT 标记 + 整点已过 | 改后等下一整点；或加 `fireImmediate` |
