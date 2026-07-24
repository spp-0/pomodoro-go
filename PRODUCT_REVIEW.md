# PomodoroNotifier 产品诊断报告

> 视角：高级产品经理（Senior PM）
> 日期：2026-07-20
> 结论定位：一个**功能闭环完整、工程实现扎实的 MVP**，但**离"可让普通用户长期稳定使用"还差关键几步**。当前更像开发者自用工具，而非面向终端用户的产品。

> ⚠️ **文档状态说明（2026-07-24）**：本报告原列出的 **P0 三项已全部修复**（见 §1 各条 ✅ 标记，对应 M1 里程碑）。后续 M2/M3 功能（提醒音、托盘内嵌设置面板、timepoint 独立文案、配置热重载保留番茄钟、时区正确显示、异步取诗词，以及统计/天气/托盘倒计时/Snooze/勿扰等）也已落地。本报告保留作为问题追踪历史。

---

## 0. 总体健康度（满分 10）

| 维度 | 评分 | 说明 |
|---|---|---|
| 功能完成度 | 7.5 | 番茄钟 + 时间点双源、托盘、精美弹窗、诗词兜底，核心闭环已通 |
| 健壮性 | 8.0 | P0 三项已全部修复（见 §1 状态），不再有"起不来 / 收不到提醒"的致命问题 |
| 用户体验 | 7.5 | M2/M3 已补齐提醒音、托盘内嵌设置面板、timepoint 独立文案、时区正确显示、弹窗徽章中文化 |
| 可维护性 | 7.0 | 文档优秀；已补 scheduler / weather 单测；依赖仍偏重（webview2 fork 传递依赖） |
| **综合** | **7.0** | P0 已清，进入 P1/P2 体验打磨阶段 |

---

## 1. P0 — 必须修复（**已全部修复 ✅**，对应 M1 里程碑）

### 1.1 【严重 Bug】`once_per_day=false` 时，同一分钟内每秒弹窗一次（刷屏）
- **位置**：`internal/scheduler/scheduler.go` `tickTimepoints()`，约第 282–291 行
- **现象**：时间点触发条件是 `now.Hour()==h && now.Minute()==m`，命中后若该分钟内的每一秒都满足条件，而 `once_per_day=false` 时跳过了 `lastT` 去重，于是**整分钟内每秒触发一次**，约 60 个弹窗堆叠。
- **根因**：去重逻辑被 `once_per_day` 开关错误耦合。事实上 `lastT` 已按 `天+分钟` 去重，本身就是"每天一次"，`once_per_day` 字段名与行为不符。
- **建议**：
  - 去重**始终**基于 `lastT[dayKey][minKey]`，与 `once_per_day` 无关；
  - 重新定义 `once_per_day` 语义（或直接移除该字段），并在文档里澄清。

> ✅ **已修复（2026-07-24，M1）**：去重已与 `once_per_day` 解耦，始终基于 `lastT[dayKey][minKey]`（scheduler.go:400-410），每天每点仅触发一次。

### 1.2 【严重 Bug】配置文件 JSON 写错一个字符，工具直接退出的
- **位置**：`cmd/pomodoro-agent/main.go` 第 60–70 行
- **现象**：`config.Load` 除"文件不存在"外，任何解析错误都走 `fail()` → `os.Exit(1)`。普通用户改 `config.json` 多一个逗号，托盘常驻程序直接起不来，且只弹一个原始错误框。
- **建议**：解析失败时**降级为默认配置 + 弹窗告警**，保证工具永远能启动；同时把出错字段位置提示给用户（用 `json.SyntaxError` 的行号）。

### 1.3 【严重可靠性】WebView2 不可用 / 失败时，提醒静默丢失
- **位置**：`cmd/pomodoro-agent/main.go` `startUIDispatcher()`（第 251–256 行）；`internal/ui/popup.go` `ShowPopup` 返回 error
- **现象**：`ShowPopup` 失败仅打一行日志，事件被丢弃。**提醒类工具"静默失败"是不可接受的**——用户以为设了提醒，实际什么都没收到。
- **建议**：
  - `ShowPopup` 失败时回退到 `systray.ShowNotification`（气泡）或 `MessageBoxW`；
  - 启动阶段检测 WebView2 Runtime 是否存在，缺失时主动提示并给出下载链接（文档已有链接）。

> ✅ **已修复（2026-07-24，M1）**：`ShowPopup` 失败时回退 `showInfo` 系统消息框兜底提醒，并提示可能缺少 WebView2 Runtime（main.go:375-380）。

---

## 2. P1 — 高优先级（体验与可靠性瓶颈）

### 2.1 配置热重载会打断进行中的番茄钟
- **位置**：`scheduler.go` `UpdateConfig()` 第 80 行 `s.pomo = pomodoroState{}`
- **问题**：用户编辑 `config.json`（哪怕只是改时间点），正在计时的番茄钟被清零重置。
- **建议**：热重载仅更新参数（时长、文案、开关），**保留当前 phase / nextAt / lastWorkDay**，让进行中的番茄钟不受影响。

### 2.2 弹窗时间显示用的是 `time.Local` 而非配置时区
- **位置**：`internal/ui/popup.go` 第 55 行 `e.At.In(time.Local)`
- **问题**：调度按 `cfg.TimeZone` 触发，但弹窗里"触发时间"却用本机时区显示，时区配置下会显示错误时间。
- **建议**：弹窗时间随配置时区显示（把 `loc` 透传或存入事件）。

### 2.3 没有任何提醒音
- **位置**：全代码库——`PopupConfig` 无声音字段，`ShowPopup` 不发声
- **问题**：用户离开屏幕或当前窗口未聚焦时，纯视觉弹窗极易错过。对"提醒"类产品这是核心能力缺失。
- **建议**：新增 `sound` 配置（开关 + 可选 wav/内置音），用 `winmm.PlaySound` 播放；默认轻提示音。

### 2.4 诗词拉取同步阻塞调度协程（最长 3 秒）
- **位置**：`main.go` `emit` 闭包（第 110–137 行）内 `fetchQuote()`（第 315–331 行）
- **问题**：`emit` 在每秒一次的 tick 协程里同步做 HTTP 取诗词，断网/慢网时最长阻塞 `quote_timeout`（配置 3s），期间调度卡住、后续弹窗延迟。
- **建议**：把 quote 拉取移到 UI dispatcher 内、或 `emit` 先投递事件、`fetchQuote` 异步完成后由 UI 填充；降低默认超时到 1.5s。

### 2.5 无设置界面，所有配置需手改 JSON
- **位置**：整体 UX（README §3.3 要求编辑 `config.json`）
- **问题**：一个定位"终端用户"的常驻工具，却要求用户手写 JSON。这是 adoption 最大障碍。
- **建议（按性价比排序）**：
  1. 先做"系统托盘 → 设置"内嵌极简面板（用 WebView2 复用现有能力）；
  2. 至少提供"开机自启开关""开启/关闭声音""番茄钟时长"等高频项的快捷开关。

### 2.6 timepoint 仅支持单一共享文案
- **位置**：`config.TimepointConfig`（`config.go` 28–34）+ `tickTimepoints` / `fireImmediateTimepointsLocked` 用同一 `tp.Title/tp.Message`
- **问题**：多个时间点无法各自配标题/内容，"10:30 喝水 / 14:30 散步 / 18:10 收邮件"做不到，只能所有点说同一句话。
- **建议**：`times` 升级为对象数组 `[{time, title, message}]`，保留旧字符串格式做兼容解析。

---

## 3. P2 — 中低优先级（打磨 / 技术债）

| # | 问题 | 位置 | 建议 |
|---|---|---|---|
| 3.1 | `ClickThrough` 是死配置 | `config.go:40`、API.md 有文档，但 `ui.Options` 无该字段、`ShowPopup` 从未使用 | 实现或删除字段+文档 |
| 3.2 | 弹窗徽章直接显示内部枚举（`timepoint`/`pomodoro_break_start`） | `popup.go` 模板 `{{.Kind}}` | 映射为中文标签「定时提醒 / 番茄钟」 |
| 3.3 | 手动弹窗标题写死"手动测试/测试提醒" | `main.go` `emitManual` 261–267 行 | 点托盘=「立即提醒」，不应叫测试 |
| 3.4 | `emitManual` 用阻塞发送 `popupQueue`，满时卡住托盘菜单线程 | `main.go:270` | 与 `emit` 一致改非阻塞（select+default） |
| 3.5 | 手动弹窗 `auto_close` 硬编码 15s，忽略配置 | `main.go:277` | 取 `cur.Popup.AutoCloseSeconds` |
| 3.6 | 日志无滚动（无 rotate） | `logging.go`（API.md 2.3 自认） | 按大小/日期滚动，避免长期运行膨胀 |
| 3.7 | `-test` 模式在主协程直接 `ShowPopup`，未走 UI dispatcher | `main.go:84` | 统一走 `popupQueue`，符合线程约束 |
| 3.8 | 零单元测试 | 无 `_test.go` | 给 `scheduler`（番茄钟/时间点/去重/时区）补关键路径测试 |
| 3.9 | 依赖 `Krakinsight/go-webview2` 小众 fork，引入 webgpu/cbor 等重传递依赖（~10MB） | `go.mod` | 评估切换到更主流轻量绑定（如 `jchv/go-webview2`） |
| 3.10 | 文档硬编码旧路径 `c:\Users\Administrator\scripts\pomodoro-go`；`go.mod` 写 `go 1.26` 但文档说 `1.25+` | README:82、DEVELOPING:20/28 | 路径改为相对/占位，统一 Go 版本表述 |
| 3.11 | `done` 通道 + 空 goroutine 是死代码 | `popup.go:99–125` | 删除，简化关闭逻辑 |
| 3.12 | 版本号硬编码 "1.0" | `winutil.go:26`、`main.go` test | 经 `-ldflags -X` 注入，单一来源 |
| 3.13 | 不支持多显示器，弹窗只在主屏工作区 | `popup.go` `computeLocation` 143–178 | 取当前光标/活动窗口所在屏的 workarea |
| 3.14 | `SetForegroundWindow` 强制抢焦点，可能打断输入 | `popup.go` `forceForegroundPopup` 368–424 | 默认仅置顶+闪烁，抢焦点可配置 |
| 3.15 | `lastT`/`lastLog` map 无清理，长期运行缓慢增长 | `scheduler.go` | 定期清理非当天 key |

---

## 4. 产品路线图（建议发布节奏）

### 里程碑 M1 — "不会害用户"（✅ 已完成，2026-07-24）
- [x] 修 P0-1.1（时间点刷屏）
- [x] 修 P0-1.2（配置错误降级而非退出）
- [x] 修 P0-1.3（WebView2 失败兜底通知）
- [x] 补 `scheduler` 关键路径单元测试（P2-3.8）

### 里程碑 M2 — "像样地能用"（2–3 周）
- [ ] P1-2.3 提醒音（可配置）
- [ ] P1-2.5 托盘内嵌极简设置面板 + 开机自启开关
- [ ] P1-2.6 timepoint 每项独立文案
- [ ] P1-2.1 热重载不打断番茄钟
- [ ] P1-2.2 / 2.4 时区显示 + 异步取诗词

### 里程碑 M3 — "打磨成产品"（后续）
- [ ] 统计（每日完成番茄数）
- [ ] snooze 推迟提醒（文档已规划）
- [ ] 多显示器 / 不抢焦点
- [ ] 依赖瘦身、设置面板图形化、版本注入

---

## 5. 附录：问题清单（按严重度）

- **P0（3，已全部修复 ✅）**：1.1 时间点刷屏 / 1.2 配置错误退出 / 1.3 WebView2 静默失败
- **P1（6）**：2.1 热重载打断番茄钟 / 2.2 时区显示错误 / 2.3 无声音 / 2.4 同步取诗词阻塞 / 2.5 无设置 UI / 2.6 单一文案
- **P2（15）**：3.1–3.15（见上表）

> 一句话总结：**先把"会不会害用户"的三件事堵住，再给普通用户一个能改设置、能听到声音的版本，这个工具才谈得上"发布"。**
