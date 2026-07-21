# PomodoroNotifier

> 番茄钟 + 时间点提醒 + 托盘常驻 + 精美 WebView2 弹窗 + 随机诗词/名言。
> 单进程 Windows GUI 程序，零 CGO，无控制台窗口。

---

## 1. 这是什么

一个常驻任务栏托盘的小工具：

- **番茄钟循环**：工作 25 分钟 / 休息 5 分钟，到点自动提醒
- **指定时间点列表**：每天 10:30 / 14:30 / 17:30 等时间各弹一次
- **两种可同时启用**，互不影响
- **弹窗精美**：渐变背景 + 卡片 + 倒计时 + ESC/Enter 关闭
- **随机诗词/名言**：弹窗里附一句中文诗词或名人名言（在线 + 离线兜底）
- **托盘三态图标**：🔴 红色专注 / 🟡 黄色休息 / ⚪ 灰色暂停

## 2. 用户文档

- **架构与设计**：[ARCHITECTURE.md](./ARCHITECTURE.md)
- **开发指南（增删改查）**：[DEVELOPING.md](./DEVELOPING.md)
- **公开 API 速查**：[API.md](./API.md)

## 3. 快速使用

### 3.1 运行
1. 双击 `PomodoroNotifier.exe`
2. 任务栏右下角出现红色番茄图标 = 启动成功
3. 首次运行会在 exe 同目录自动生成 `config.json` 和 `pomodoro.log`

### 3.2 托盘菜单
- **左键单击托盘** / **立即弹一次**：手动弹一个提醒
- **设置…**：打开图形化设置窗口（番茄钟、时间点、提醒音、弹窗位置、诗词 API、开机自启）
- **暂停调度**（点选切换）：所有提醒暂停；再次点击恢复
- **声音**（点选切换）：弹窗时是否播放提醒音
- **开机自启**（点选切换）：登录时自动启动
- **重新加载配置**：编辑 `config.json` 后立即生效
- **打开配置目录**：在资源管理器中打开 exe 所在目录
- **关于**：版本与快捷键说明
- **退出**

### 3.3 改提醒时间
最方便：右键托盘 → **设置…** 打开设置窗口直接改（自动保存并热重载）。

也可以手动编辑 `config.json`（exe 同目录），保存后等 2 秒自动重载；或右键托盘 → 重新加载配置。
每个时间点可单独配标题/内容：

```json
{
  "pomodoro": {
    "enabled": true,
    "work_minutes": 25,
    "break_minutes": 5,
    "work_days": [1, 2, 3, 4, 5],
    "work_start": "09:00",
    "work_end":   "18:00",
    "work_text":  "休息时间到！站起来活动一下、眺望远处。",
    "break_text": "休息结束，开始下一个番茄钟。"
  },
  "timepoint": {
    "enabled": true,
    "times": [
      { "time": "10:30" },
      { "time": "14:30", "title": "喝口水", "message": "起来倒杯水，远眺一会。" },
      { "time": "17:30" }
    ],
    "title":   "温馨提醒",
    "message": "到点啦，起来走走，喝口水，看看远处。"
  },
  "popup": {
    "auto_close_seconds": 20,
    "width":  560,
    "height": 340,
    "topmost": true,
    "position": "bottom-right",
    "sound": { "enabled": true, "file": "" }
  },
  "autostart": false
}
```

> 完整配置说明在 [DEVELOPING.md §3.1](./DEVELOPING.md)

### 3.4 开机自启
右键托盘 → **开机自启**（点选切换）即可，会自动写入注册表 `HKCU\...\Run`。

也可手动：把 `PomodoroNotifier.exe` 的快捷方式放到 `shell:startup`（Win+R → `shell:startup` → 回车）；
更稳的方式：任务计划程序 → 创建任务 → 触发器"用户登录时" → 操作"启动程序" → 勾选"使用最高权限"。

## 4. 编译（开发者）

```powershell
cd <项目目录>\pomodoro-go
go mod tidy
go build -ldflags "-H windowsgui -s -w" -o .\dist\PomodoroNotifier.exe .\cmd\pomodoro-agent
```

产物：`dist\PomodoroNotifier.exe`（~10MB，已用 `-s -w` 去符号表）

## 5. 常见问题

| 问题 | 解决 |
|---|---|
| 弹窗没显示 | 检查 `dist\pomodoro.log`；确认 WebView2 Runtime 已装 |
| 改配置没生效 | 右键托盘 → 重新加载配置 |
| 位置不对 | 改 `popup.position`：center / top-left / top-right / bottom-left / bottom-right |
| 不想要番茄钟 | `"pomodoro.enabled": false` |
| 不想要时间点 | `"timepoint.enabled": false` |
| 想换诗词/名言源 | 编辑 `internal/quote/quote.go` 或改 `quote_api.url` |
| 想看更详细日志 | 看 `dist\pomodoro.log`，`[tick]` `[emit]` `[ui]` 三类关键字 |

## 6. 目录速览

```
pomodoro-go/
├── cmd/
│   ├── pomodoro-agent/         # 主程序入口
│   └── gentray/                # 托盘图标生成工具
├── internal/
│   ├── config/                 # 配置加载
│   ├── logging/                # 文件日志
│   ├── quote/                  # 诗词/名言
│   ├── scheduler/              # 调度器
│   └── ui/                     # WebView2 弹窗
├── dist/                       # 编译产物
├── ARCHITECTURE.md             # 架构文档
├── API.md                      # API 速查
├── DEVELOPING.md               # 开发指南
└── README.md                   # 本文件
```

## 7. 技术栈

- Go 1.26+
- [gogpu/systray](https://github.com/gogpu/systray) — 系统托盘（零 CGO）
- [Krakinsight/go-webview2](https://github.com/Krakinsight/go-webview2) — WebView2 绑定
- v1.hitokoto.cn — 在线诗词/名言 API

## 8. 反馈

修改/扩展功能请看 [DEVELOPING.md](./DEVELOPING.md)。
遇到 bug 排查流程见 [DEVELOPING.md §4](./DEVELOPING.md)。
