// Package i18n 提供 PomodoroNotifier 的界面多语言支持。
//
// 设计要点：
//   - 只翻译「界面外壳」文案（菜单、设置、弹窗按钮、天气、提示）；
//     用户自己配置的提醒内容（timepoint 文案、诗词）保持原样，不做机翻。
//   - T(lang, key, args...) 做 map 查表，缺失时回落 zh-CN，再回落 key 本身，
//     保证任何 key 缺失都不会让界面出现空白。
//   - 天气文案按 WMO code 分组（weather.<group>），emoji 与语言无关。
package i18n

// Lang 是受支持的语言代码。
type Lang string

const (
	ZhCN Lang = "zh-CN"
	ZhTW Lang = "zh-TW"
	En   Lang = "en"
	Ja   Lang = "ja"
	Ko   Lang = "ko"
)

var supported = []Lang{ZhCN, ZhTW, En, Ja, Ko}

// Supported 返回所有受支持的语言（顺序即设置下拉顺序）。
func Supported() []Lang { return supported }

// Name 返回语言的本地化显示名（用于设置下拉）。
func Name(l Lang) string {
	switch l {
	case ZhCN:
		return "简体中文"
	case ZhTW:
		return "繁體中文"
	case En:
		return "English"
	case Ja:
		return "日本語"
	case Ko:
		return "한국어"
	default:
		return string(l)
	}
}

// norm 把任意输入规范化为受支持语言，未知值回落 zh-CN。
func norm(l Lang) Lang {
	return Norm(l)
}

// Norm 规范化为受支持语言（未知回落 zh-CN），供包外使用。
func Norm(l Lang) Lang {
	for _, s := range supported {
		if s == l {
			return l
		}
	}
	return ZhCN
}

// T 查表翻译（纯查表，不做格式化）。
// 缺失时回落 zh-CN，再回落 key 本身，保证永不返回空串以外的「报错」。
// 需要带参数（%d 等）时，调用方用 fmt.Sprintf(T(lang, key), args...) 包裹，
// 这样本函数签名不会被 go vet 当成 printf，动态 key 也不会告警。
func T(l Lang, key string) string {
	l = norm(l)
	if m, ok := dict[l]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if m, ok := dict[ZhCN]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return key
}

// ---------- 天气：WMO code → emoji + 文案分组 ----------

var weatherEmoji = map[int]string{
	0:  "☀️",
	1:  "🌤️",
	2:  "⛅",
	3:  "☁️",
	45: "🌫️", 48: "🌫️",
	51: "🌦️", 53: "🌦️", 55: "🌦️",
	56: "🌧️", 57: "🌧️",
	61: "🌧️", 63: "🌧️", 65: "🌧️",
	66: "🌧️", 67: "🌧️",
	71: "🌨️", 73: "🌨️", 75: "🌨️",
	77: "🌨️",
	80: "🌦️", 81: "🌦️", 82: "🌦️",
	85: "🌨️", 86: "🌨️",
	95: "⛈️",
	96: "⛈️", 99: "⛈️",
}

// codeGroup 把 WMO code 归并到文案分组键。
var codeGroup = map[int]string{
	0:  "clear",
	1:  "mainly_clear",
	2:  "partly_cloudy",
	3:  "overcast",
	45: "fog", 48: "fog",
	51: "drizzle", 53: "drizzle", 55: "drizzle",
	56: "freezing_drizzle", 57: "freezing_drizzle",
	61: "rain", 63: "rain", 65: "rain",
	66: "freezing_rain", 67: "freezing_rain",
	71: "snow", 73: "snow", 75: "snow",
	77: "snow_grains",
	80: "rain_showers", 81: "rain_showers", 82: "rain_showers",
	85: "snow_showers", 86: "snow_showers",
	95: "thunderstorm", 96: "thunderstorm_hail", 99: "thunderstorm_hail",
}

// WeatherText 返回指定语言下的天气 emoji 与文案。
func WeatherText(l Lang, code int) (emoji, text string) {
	e, ok := weatherEmoji[code]
	if !ok {
		e = "🌡️"
	}
	g, ok := codeGroup[code]
	if !ok {
		g = "unknown"
	}
	return e, T(l, "weather."+g)
}

// Dict 返回某语言的完整字典（该语言条目覆盖在 zh-CN 之上），
// 供设置面板把文案整体注入前端、由 JS 的 t(key) 做查表。
func Dict(l Lang) map[string]string {
	l = norm(l)
	out := make(map[string]string)
	for k, v := range dict[ZhCN] {
		out[k] = v
	}
	if l != ZhCN {
		for k, v := range dict[l] {
			out[k] = v
		}
	}
	return out
}

var dict = map[Lang]map[string]string{
	ZhCN: {
		// 托盘
		"tray.tooltip.normal":  "🍅 PomodoroNotifier",
		"tray.tooltip.idle":    "🍅 PomodoroNotifier - 待命中",
		"tray.tooltip.work":    "🍅 PomodoroNotifier - 专注中",
		"tray.tooltip.break":   "🍅 PomodoroNotifier - 休息中",
		"tray.tooltip.paused":  "🍅 PomodoroNotifier - 已暂停",
		"tray.tooltip.dnd":     "🌙 PomodoroNotifier - 勿扰中（会议）",
		"tray.menu.popup":      "立即弹一次",
		"tray.menu.settings":   "设置…",
		"tray.menu.pause":      "暂停调度（点击切换）",
		"tray.menu.dnd":        "勿扰模式（会议）",
		"tray.menu.skip":       "跳过当前休息",
		"tray.menu.extend":     "延长休息 5 分钟",
		"tray.menu.sound":      "声音",
		"tray.menu.autostart":  "开机自启",
		"tray.menu.reload":     "重新加载配置",
		"tray.menu.opendir":    "打开配置目录",
		"tray.menu.about":      "关于",
		"tray.menu.exit":       "退出",
		"tray.notify.dnd_on":   "已进入勿扰模式，提醒暂不弹窗（番茄钟继续计时）",
		"tray.notify.dnd_off":  "已退出勿扰模式",
		"tray.notify.reloaded": "配置已重新加载",
		"tray.status.work":     "🍅 专注中 · 剩余 %s · 今日 %d🍅",
		"tray.status.break":    "☕ 休息中 · 剩余 %s · 今日 %d🍅",
		"tray.status.idle":     "🍅 待命 · 今日 %d🍅",

		// 系统消息框
		"info.skip.title":      "跳过休息",
		"info.skip.msg":        "当前不在休息阶段，无法跳过。",
		"info.extend.title":    "延长休息",
		"info.extend.msg":      "当前不在休息阶段，无法延长。",
		"info.sound.title":     "声音",
		"info.sound.msg":       "保存失败: ",
		"info.autostart.title": "开机自启",
		"info.autostart.msg":   "设置失败: ",
		"info.reload.title":    "重新加载失败",
		"info.webview.title":   "提醒组件异常",
		"info.webview.msg":     "弹窗渲染失败，已用系统消息框兜底提醒。\n可能缺少 WebView2 Runtime，请安装后重启。",
		"fail.title":           "PomodoroNotifier 启动失败",
		"cfgerror.title":       "配置有误",
		"cfgerror.msg":         "配置文件存在错误，已使用默认配置。\n请检查: ",

		// 弹窗外壳
		"popup.badge.pomodoro_break_start": "休息提醒",
		"popup.badge.pomodoro_break_end":   "开始专注",
		"popup.badge.timepoint":            "定时提醒",
		"popup.badge.manual":               "手动提醒",
		"popup.badge.test":                 "测试",
		"popup.badge.default":              "提醒",
		"popup.title_fallback":             "提醒",
		"popup.msg_fallback":               "到点啦，起来活动一下。",
		"popup.weather_loading":            "正在获取天气…",
		"popup.weather_error":              "天气获取失败",
		"popup.snooze_label":               "稍后提醒",
		"popup.snooze_5":                   "5 分钟",
		"popup.snooze_10":                  "10 分钟",
		"popup.snooze_15":                  "15 分钟",
		"popup.auto_close":                 "秒后自动关闭",
		"popup.close":                      "知道了",
		"popup.data_error":                 "弹窗数据解析失败: ",

		"manual.msg":                 "这是你点击托盘触发的提醒。",
		"pomodoro.break_start_title": "🍅 休息时间到！",
		"pomodoro.break_end_title":   "🍅 休息结束",

		// 设置面板
		"settings.title":               "⚙️ PomodoroNotifier 设置",
		"settings.language":            "显示语言",
		"settings.language.hint":       "界面语言；提醒内容与诗词保持原样。",
		"settings.sec.pomodoro":        "番茄钟",
		"settings.pomodoro.enabled":    "启用番茄钟循环",
		"settings.pomodoro.work":       "工作",
		"settings.pomodoro.minutes":    "分钟",
		"settings.pomodoro.break":      "休息",
		"settings.pomodoro.presets":    "快捷预设",
		"settings.sec.timepoint":       "时间点提醒",
		"settings.timepoint.enabled":   "启用指定时间点提醒",
		"settings.timepoint.add":       "+ 添加时间点",
		"settings.timepoint.hint":      "时间格式 HH:MM；标题/内容留空则使用下方默认文案。",
		"settings.sec.sound":           "提醒音",
		"settings.sound.enabled":       "弹窗时播放提醒音",
		"settings.sec.weather":         "天气（弹窗内显示）",
		"settings.weather.enabled":     "弹窗内显示天气",
		"settings.weather.city":        "城市",
		"settings.weather.placeholder": "如 北京 / 上海",
		"settings.weather.hint":        "使用 Open-Meteo 免费接口，无需密钥；断网时自动隐藏。",
		"settings.sec.popup_quote":     "弹窗与诗词",
		"settings.popup.position":      "弹窗位置",
		"settings.pos.center":          "居中",
		"settings.pos.topleft":         "左上",
		"settings.pos.topright":        "右上",
		"settings.pos.bottomleft":      "左下",
		"settings.pos.bottomright":     "右下",
		"settings.quote.api":           "诗词API",
		"settings.quote.timeout":       "超时",
		"settings.sec.stats":           "统计",
		"settings.stats.today":         "今日完成",
		"settings.stats.pomodoros":     "个番茄钟",
		"settings.stats.last7":         "最近 7 天",
		"settings.sec.other":           "其他",
		"settings.other.autostart":     "开机自启",
		"settings.btn.cancel":          "取消",
		"settings.btn.save":            "保存",
		"settings.err.parse_cfg":       "配置解析失败: ",
		"settings.err.parse_form":      "表单数据解析失败: ",
		"settings.err.minutes":         "工作/休息分钟数必须大于 0",
		"settings.err.timepoint_fmt":   "时间点格式错误（应为 HH:MM）: ",
		"settings.err.timeout_fmt":     "诗词超时格式错误（如 1500ms）: ",
		"settings.err.save":            "保存失败: ",

		// 调度器生成的提醒文案（前缀为 UI，后缀为用户内容）
		"pomodoro.work_prefix": "工作了 %d 分钟，休息 %d 分钟。",

		// 内置默认提醒文案（随语言变化；用户已自定义则不改）
		"default.pomodoro_work":     "休息时间到！站起来活动一下、眺望远处。",
		"default.pomodoro_break":    "休息结束，开始下一个番茄钟。",
		"default.timepoint_title":   "温馨提醒",
		"default.timepoint_message": "到点啦，起来走走，喝口水，看看远处。",

		// 关于
		"about.title": "关于 PomodoroNotifier",
		"about.body":  "PomodoroNotifier 1.0\n\n• 番茄钟循环 + 指定时间点提醒\n• 随机诗词/名言（在线 + 离线兜底）\n• WebView2 精美弹窗\n\n左键托盘 = 立即弹一次\n右键托盘 = 菜单",

		// 天气文案（按 WMO 分组）
		"weather.clear":             "晴",
		"weather.mainly_clear":      "晴间多云",
		"weather.partly_cloudy":     "局部多云",
		"weather.overcast":          "阴",
		"weather.fog":               "雾",
		"weather.drizzle":           "毛毛雨",
		"weather.freezing_drizzle":  "冻毛毛雨",
		"weather.rain":              "雨",
		"weather.freezing_rain":     "冻雨",
		"weather.snow":              "雪",
		"weather.snow_grains":       "雪粒",
		"weather.rain_showers":      "阵雨",
		"weather.snow_showers":      "阵雪",
		"weather.thunderstorm":      "雷阵雨",
		"weather.thunderstorm_hail": "雷阵雨伴冰雹",
		"weather.unknown":           "未知",
	},

	ZhTW: {
		"tray.tooltip.normal":  "🍅 PomodoroNotifier",
		"tray.tooltip.idle":    "🍅 PomodoroNotifier - 待命中",
		"tray.tooltip.work":    "🍅 PomodoroNotifier - 專注中",
		"tray.tooltip.break":   "🍅 PomodoroNotifier - 休息中",
		"tray.tooltip.paused":  "🍅 PomodoroNotifier - 已暫停",
		"tray.tooltip.dnd":     "🌙 PomodoroNotifier - 勿擾中（會議）",
		"tray.menu.popup":      "立即彈一次",
		"tray.menu.settings":   "設定…",
		"tray.menu.pause":      "暫停排程（點擊切換）",
		"tray.menu.dnd":        "勿擾模式（會議）",
		"tray.menu.skip":       "跳過當前休息",
		"tray.menu.extend":     "延長休息 5 分鐘",
		"tray.menu.sound":      "聲音",
		"tray.menu.autostart":  "開機自啟",
		"tray.menu.reload":     "重新載入設定",
		"tray.menu.opendir":    "開啟設定目錄",
		"tray.menu.about":      "關於",
		"tray.menu.exit":       "退出",
		"tray.notify.dnd_on":   "已進入勿擾模式，提醒暫不彈窗（番茄鐘繼續計時）",
		"tray.notify.dnd_off":  "已退出勿擾模式",
		"tray.notify.reloaded": "設定已重新載入",
		"tray.status.work":     "🍅 專注中 · 剩餘 %s · 今日 %d🍅",
		"tray.status.break":    "☕ 休息中 · 剩餘 %s · 今日 %d🍅",
		"tray.status.idle":     "🍅 待命 · 今日 %d🍅",

		"info.skip.title":      "跳過休息",
		"info.skip.msg":        "當前不在休息階段，無法跳過。",
		"info.extend.title":    "延長休息",
		"info.extend.msg":      "當前不在休息階段，無法延長。",
		"info.sound.title":     "聲音",
		"info.sound.msg":       "儲存失敗: ",
		"info.autostart.title": "開機自啟",
		"info.autostart.msg":   "設定失敗: ",
		"info.reload.title":    "重新載入失敗",
		"info.webview.title":   "提醒元件異常",
		"info.webview.msg":     "彈窗渲染失敗，已用系統訊息框兜底提醒。\n可能缺少 WebView2 Runtime，請安裝後重啟。",
		"fail.title":           "PomodoroNotifier 啟動失敗",
		"cfgerror.title":       "設定有誤",
		"cfgerror.msg":         "設定檔存在錯誤，已使用預設設定。\n請檢查: ",

		"popup.badge.pomodoro_break_start": "休息提醒",
		"popup.badge.pomodoro_break_end":   "開始專注",
		"popup.badge.timepoint":            "定時提醒",
		"popup.badge.manual":               "手動提醒",
		"popup.badge.test":                 "測試",
		"popup.badge.default":              "提醒",
		"popup.title_fallback":             "提醒",
		"popup.msg_fallback":               "到點啦，起來活動一下。",
		"popup.weather_loading":            "正在獲取天氣…",
		"popup.weather_error":              "天氣獲取失敗",
		"popup.snooze_label":               "稍後提醒",
		"popup.snooze_5":                   "5 分鐘",
		"popup.snooze_10":                  "10 分鐘",
		"popup.snooze_15":                  "15 分鐘",
		"popup.auto_close":                 "秒後自動關閉",
		"popup.close":                      "知道了",
		"popup.data_error":                 "彈窗資料解析失敗: ",

		"settings.title":               "⚙️ PomodoroNotifier 設定",
		"settings.language":            "顯示語言",
		"settings.language.hint":       "介面語言；提醒內容與詩詞保持原樣。",
		"settings.sec.pomodoro":        "番茄鐘",
		"settings.pomodoro.enabled":    "啟用番茄鐘循環",
		"settings.pomodoro.work":       "工作",
		"settings.pomodoro.minutes":    "分鐘",
		"settings.pomodoro.break":      "休息",
		"settings.pomodoro.presets":    "快捷預設",
		"settings.sec.timepoint":       "時間點提醒",
		"settings.timepoint.enabled":   "啟用指定時間點提醒",
		"settings.timepoint.add":       "+ 新增時間點",
		"settings.timepoint.hint":      "時間格式 HH:MM；標題/內容留空則使用下方預設文案。",
		"settings.sec.sound":           "提醒音",
		"settings.sound.enabled":       "彈窗時播放提醒音",
		"settings.sec.weather":         "天氣（彈窗內顯示）",
		"settings.weather.enabled":     "彈窗內顯示天氣",
		"settings.weather.city":        "城市",
		"settings.weather.placeholder": "如 北京 / 上海",
		"settings.weather.hint":        "使用 Open-Meteo 免費介面，無需金鑰；斷網時自動隱藏。",
		"settings.sec.popup_quote":     "彈窗與詩詞",
		"settings.popup.position":      "彈窗位置",
		"settings.pos.center":          "居中",
		"settings.pos.topleft":         "左上",
		"settings.pos.topright":        "右上",
		"settings.pos.bottomleft":      "左下",
		"settings.pos.bottomright":     "右下",
		"settings.quote.api":           "詩詞API",
		"settings.quote.timeout":       "逾時",
		"settings.sec.stats":           "統計",
		"settings.stats.today":         "今日完成",
		"settings.stats.pomodoros":     "個番茄鐘",
		"settings.stats.last7":         "最近 7 天",
		"settings.sec.other":           "其他",
		"settings.other.autostart":     "開機自啟",
		"settings.btn.cancel":          "取消",
		"settings.btn.save":            "儲存",
		"settings.err.parse_cfg":       "設定解析失敗: ",
		"settings.err.parse_form":      "表單資料解析失敗: ",
		"settings.err.minutes":         "工作/休息分鐘數必須大於 0",
		"settings.err.timepoint_fmt":   "時間點格式錯誤（應為 HH:MM）: ",
		"settings.err.timeout_fmt":     "詩詞逾時格式錯誤（如 1500ms）: ",
		"settings.err.save":            "儲存失敗: ",

		"pomodoro.work_prefix": "工作了 %d 分鐘，休息 %d 分鐘。",

		"default.pomodoro_work":     "休息時間到！站起來活動一下、眺望遠處。",
		"default.pomodoro_break":    "休息結束，開始下一個番茄鐘。",
		"default.timepoint_title":   "溫馨提醒",
		"default.timepoint_message": "到點啦，起來走走，喝口水，看看遠處。",

		"manual.msg":                 "這是你點擊托盤觸發的提醒。",
		"pomodoro.break_start_title": "🍅 休息時間到！",
		"pomodoro.break_end_title":   "🍅 休息結束",

		"about.title": "關於 PomodoroNotifier",
		"about.body":  "PomodoroNotifier 1.0\n\n• 番茄鐘循環 + 指定時間點提醒\n• 隨機詩詞/名言（線上 + 離線兜底）\n• WebView2 精美彈窗\n\n左鍵托盤 = 立即彈一次\n右鍵托盤 = 選單",

		"weather.clear":             "晴",
		"weather.mainly_clear":      "晴間多雲",
		"weather.partly_cloudy":     "局部多雲",
		"weather.overcast":          "陰",
		"weather.fog":               "霧",
		"weather.drizzle":           "毛毛雨",
		"weather.freezing_drizzle":  "凍毛毛雨",
		"weather.rain":              "雨",
		"weather.freezing_rain":     "凍雨",
		"weather.snow":              "雪",
		"weather.snow_grains":       "雪粒",
		"weather.rain_showers":      "陣雨",
		"weather.snow_showers":      "陣雪",
		"weather.thunderstorm":      "雷陣雨",
		"weather.thunderstorm_hail": "雷陣雨伴冰雹",
		"weather.unknown":           "未知",
	},

	En: {
		"tray.tooltip.normal":  "🍅 PomodoroNotifier",
		"tray.tooltip.idle":    "🍅 PomodoroNotifier - Idle",
		"tray.tooltip.work":    "🍅 PomodoroNotifier - Focusing",
		"tray.tooltip.break":   "🍅 PomodoroNotifier - On break",
		"tray.tooltip.paused":  "🍅 PomodoroNotifier - Paused",
		"tray.tooltip.dnd":     "🌙 PomodoroNotifier - DND (meeting)",
		"tray.menu.popup":      "Popup now",
		"tray.menu.settings":   "Settings…",
		"tray.menu.pause":      "Pause schedule (toggle)",
		"tray.menu.dnd":        "DND mode (meeting)",
		"tray.menu.skip":       "Skip current break",
		"tray.menu.extend":     "Extend break 5 min",
		"tray.menu.sound":      "Sound",
		"tray.menu.autostart":  "Start on boot",
		"tray.menu.reload":     "Reload config",
		"tray.menu.opendir":    "Open config folder",
		"tray.menu.about":      "About",
		"tray.menu.exit":       "Exit",
		"tray.notify.dnd_on":   "DND enabled: popups paused, pomodoro keeps running",
		"tray.notify.dnd_off":  "Exited DND mode",
		"tray.notify.reloaded": "Config reloaded",
		"tray.status.work":     "🍅 Focusing · %s left · %d🍅 today",
		"tray.status.break":    "☕ On break · %s left · %d🍅 today",
		"tray.status.idle":     "🍅 Idle · %d🍅 today",

		"info.skip.title":      "Skip break",
		"info.skip.msg":        "Not in a break right now, cannot skip.",
		"info.extend.title":    "Extend break",
		"info.extend.msg":      "Not in a break right now, cannot extend.",
		"info.sound.title":     "Sound",
		"info.sound.msg":       "Save failed: ",
		"info.autostart.title": "Autostart",
		"info.autostart.msg":   "Setting failed: ",
		"info.reload.title":    "Reload failed",
		"info.webview.title":   "UI error",
		"info.webview.msg":     "Popup render failed; fell back to a system dialog.\nWebView2 Runtime may be missing — please install and restart.",
		"fail.title":           "PomodoroNotifier failed to start",
		"cfgerror.title":       "Config error",
		"cfgerror.msg":         "Config file has errors; default config is used.\nPlease check: ",

		"popup.badge.pomodoro_break_start": "Break reminder",
		"popup.badge.pomodoro_break_end":   "Focus time",
		"popup.badge.timepoint":            "Scheduled",
		"popup.badge.manual":               "Manual",
		"popup.badge.test":                 "Test",
		"popup.badge.default":              "Reminder",
		"popup.title_fallback":             "Reminder",
		"popup.msg_fallback":               "Time's up — stand up and stretch.",
		"popup.weather_loading":            "Loading weather…",
		"popup.weather_error":              "Weather unavailable",
		"popup.snooze_label":               "Snooze",
		"popup.snooze_5":                   "5 min",
		"popup.snooze_10":                  "10 min",
		"popup.snooze_15":                  "15 min",
		"popup.auto_close":                 "s until auto-close",
		"popup.close":                      "Got it",
		"popup.data_error":                 "Popup data parse error: ",

		"settings.title":               "⚙️ PomodoroNotifier Settings",
		"settings.language":            "Display language",
		"settings.language.hint":       "UI language; reminder text and quotes stay as-is.",
		"settings.sec.pomodoro":        "Pomodoro",
		"settings.pomodoro.enabled":    "Enable pomodoro loop",
		"settings.pomodoro.work":       "Work",
		"settings.pomodoro.minutes":    "min",
		"settings.pomodoro.break":      "Break",
		"settings.pomodoro.presets":    "Quick presets",
		"settings.sec.timepoint":       "Time-point reminders",
		"settings.timepoint.enabled":   "Enable scheduled reminders",
		"settings.timepoint.add":       "+ Add time point",
		"settings.timepoint.hint":      "Time format HH:MM; leave title/text empty to use the defaults below.",
		"settings.sec.sound":           "Notification sound",
		"settings.sound.enabled":       "Play sound on popup",
		"settings.sec.weather":         "Weather (in popup)",
		"settings.weather.enabled":     "Show weather in popup",
		"settings.weather.city":        "City",
		"settings.weather.placeholder": "e.g. Beijing / Shanghai",
		"settings.weather.hint":        "Uses Open-Meteo (free, no key); auto-hidden when offline.",
		"settings.sec.popup_quote":     "Popup & quotes",
		"settings.popup.position":      "Popup position",
		"settings.pos.center":          "Center",
		"settings.pos.topleft":         "Top-left",
		"settings.pos.topright":        "Top-right",
		"settings.pos.bottomleft":      "Bottom-left",
		"settings.pos.bottomright":     "Bottom-right",
		"settings.quote.api":           "Quote API",
		"settings.quote.timeout":       "Timeout",
		"settings.sec.stats":           "Stats",
		"settings.stats.today":         "Completed today",
		"settings.stats.pomodoros":     "pomodoros",
		"settings.stats.last7":         "Last 7 days",
		"settings.sec.other":           "Other",
		"settings.other.autostart":     "Start on boot",
		"settings.btn.cancel":          "Cancel",
		"settings.btn.save":            "Save",
		"settings.err.parse_cfg":       "Config parse error: ",
		"settings.err.parse_form":      "Form parse error: ",
		"settings.err.minutes":         "Work/break minutes must be > 0",
		"settings.err.timepoint_fmt":   "Time-point format error (expected HH:MM): ",
		"settings.err.timeout_fmt":     "Quote timeout format error (e.g. 1500ms): ",
		"settings.err.save":            "Save failed: ",

		"pomodoro.work_prefix": "Worked %d min, break %d min. ",

		"default.pomodoro_work":     "Break time! Stand up, stretch, and look into the distance.",
		"default.pomodoro_break":    "Break over — start the next pomodoro.",
		"default.timepoint_title":   "Friendly reminder",
		"default.timepoint_message": "Time's up — stretch, grab some water, and look away.",

		"manual.msg":                 "This is the reminder you triggered by clicking the tray.",
		"pomodoro.break_start_title": "🍅 Break time!",
		"pomodoro.break_end_title":   "🍅 Break over",

		"about.title": "About PomodoroNotifier",
		"about.body":  "PomodoroNotifier 1.0\n\n• Pomodoro loop + scheduled reminders\n• Random quotes (online + offline fallback)\n• WebView2 polished popups\n\nLeft-click tray = popup now\nRight-click tray = menu",

		"weather.clear":             "Clear",
		"weather.mainly_clear":      "Mainly clear",
		"weather.partly_cloudy":     "Partly cloudy",
		"weather.overcast":          "Overcast",
		"weather.fog":               "Fog",
		"weather.drizzle":           "Drizzle",
		"weather.freezing_drizzle":  "Freezing drizzle",
		"weather.rain":              "Rain",
		"weather.freezing_rain":     "Freezing rain",
		"weather.snow":              "Snow",
		"weather.snow_grains":       "Snow grains",
		"weather.rain_showers":      "Rain showers",
		"weather.snow_showers":      "Snow showers",
		"weather.thunderstorm":      "Thunderstorm",
		"weather.thunderstorm_hail": "Thunderstorm w/ hail",
		"weather.unknown":           "Unknown",
	},

	Ja: {
		"tray.tooltip.normal":  "🍅 PomodoroNotifier",
		"tray.tooltip.idle":    "🍅 PomodoroNotifier - 待機中",
		"tray.tooltip.work":    "🍅 PomodoroNotifier - 集中中",
		"tray.tooltip.break":   "🍅 PomodoroNotifier - 休憩中",
		"tray.tooltip.paused":  "🍅 PomodoroNotifier - 一時停止",
		"tray.tooltip.dnd":     "🌙 PomodoroNotifier - 通知停止中（会議）",
		"tray.menu.popup":      "今すぐ表示",
		"tray.menu.settings":   "設定…",
		"tray.menu.pause":      "スケジュール停止（切替）",
		"tray.menu.dnd":        "通知停止モード（会議）",
		"tray.menu.skip":       "現在の休憩をスキップ",
		"tray.menu.extend":     "休憩を5分延長",
		"tray.menu.sound":      "音",
		"tray.menu.autostart":  "起動時実行",
		"tray.menu.reload":     "設定を再読込",
		"tray.menu.opendir":    "設定フォルダを開く",
		"tray.menu.about":      "について",
		"tray.menu.exit":       "終了",
		"tray.notify.dnd_on":   "通知停止を開始：ポップアップ停止、ポモドーロは継続",
		"tray.notify.dnd_off":  "通知停止を終了",
		"tray.notify.reloaded": "設定を再読込しました",
		"tray.status.work":     "🍅 集中中 · 残り %s · 今日 %d🍅",
		"tray.status.break":    "☕ 休憩中 · 残り %s · 今日 %d🍅",
		"tray.status.idle":     "🍅 待機中 · 今日 %d🍅",

		"info.skip.title":      "休憩スキップ",
		"info.skip.msg":        "現在休憩中ではないためスキップできません。",
		"info.extend.title":    "休憩延長",
		"info.extend.msg":      "現在休憩中ではないため延長できません。",
		"info.sound.title":     "音",
		"info.sound.msg":       "保存失敗: ",
		"info.autostart.title": "自動起動",
		"info.autostart.msg":   "設定失敗: ",
		"info.reload.title":    "再読込失敗",
		"info.webview.title":   "UIエラー",
		"info.webview.msg":     "ポップアップの描画に失敗、システムダイアログで代替しました。\nWebView2 Runtime が無い可能性があります。インストールして再起動してください。",
		"fail.title":           "PomodoroNotifier 起動失敗",
		"cfgerror.title":       "設定エラー",
		"cfgerror.msg":         "設定ファイルに誤りがあり、既定の設定を使用します。\n確認してください: ",

		"popup.badge.pomodoro_break_start": "休憩のお知らせ",
		"popup.badge.pomodoro_break_end":   "集中開始",
		"popup.badge.timepoint":            "定時リマインダー",
		"popup.badge.manual":               "手動リマインド",
		"popup.badge.test":                 "テスト",
		"popup.badge.default":              "リマインダー",
		"popup.title_fallback":             "リマインダー",
		"popup.msg_fallback":               "時間です。立ち上がって伸びをしましょう。",
		"popup.weather_loading":            "天気を読み込み中…",
		"popup.weather_error":              "天気の取得に失敗",
		"popup.snooze_label":               "後で通知",
		"popup.snooze_5":                   "5分",
		"popup.snooze_10":                  "10分",
		"popup.snooze_15":                  "15分",
		"popup.auto_close":                 "秒後に自動閉じる",
		"popup.close":                      "了解",
		"popup.data_error":                 "ポップアップデータ解析エラー: ",

		"settings.title":               "⚙️ PomodoroNotifier 設定",
		"settings.language":            "表示言語",
		"settings.language.hint":       "UIの言語。リマインダー本文と名言はそのままです。",
		"settings.sec.pomodoro":        "ポモドーロ",
		"settings.pomodoro.enabled":    "ポモドーロループを有効化",
		"settings.pomodoro.work":       "作業",
		"settings.pomodoro.minutes":    "分",
		"settings.pomodoro.break":      "休憩",
		"settings.pomodoro.presets":    "クイックプリセット",
		"settings.sec.timepoint":       "定時リマインダー",
		"settings.timepoint.enabled":   "定時リマインダーを有効化",
		"settings.timepoint.add":       "+ 時間を追加",
		"settings.timepoint.hint":      "時刻形式 HH:MM；タイトル/本文を空にすると下の既定を使用。",
		"settings.sec.sound":           "通知音",
		"settings.sound.enabled":       "ポップアップで音を再生",
		"settings.sec.weather":         "天気（ポップアップ内）",
		"settings.weather.enabled":     "ポップアップに天気を表示",
		"settings.weather.city":        "都市",
		"settings.weather.placeholder": "例：北京 / 上海",
		"settings.weather.hint":        "Open-Meteo を利用（無料・キー不要）。オフライン時は非表示。",
		"settings.sec.popup_quote":     "ポップアップと名言",
		"settings.popup.position":      "ポップアップ位置",
		"settings.pos.center":          "中央",
		"settings.pos.topleft":         "左上",
		"settings.pos.topright":        "右上",
		"settings.pos.bottomleft":      "左下",
		"settings.pos.bottomright":     "右下",
		"settings.quote.api":           "名言API",
		"settings.quote.timeout":       "タイムアウト",
		"settings.sec.stats":           "統計",
		"settings.stats.today":         "今日の完了",
		"settings.stats.pomodoros":     "ポモドーロ",
		"settings.stats.last7":         "直近7日",
		"settings.sec.other":           "その他",
		"settings.other.autostart":     "起動時実行",
		"settings.btn.cancel":          "キャンセル",
		"settings.btn.save":            "保存",
		"settings.err.parse_cfg":       "設定解析エラー: ",
		"settings.err.parse_form":      "フォーム解析エラー: ",
		"settings.err.minutes":         "作業/休憩分は 0 より大きくしてください",
		"settings.err.timepoint_fmt":   "時刻形式エラー（HH:MM のこと）: ",
		"settings.err.timeout_fmt":     "名言タイムアウト形式エラー（例 1500ms）: ",
		"settings.err.save":            "保存失敗: ",

		"pomodoro.work_prefix": "%d分働いて、%d分休憩。",

		"default.pomodoro_work":     "休憩時間です！立ち上がって伸びをし、遠くを見ましょう。",
		"default.pomodoro_break":    "休憩終了、次のポモドーロを開始。",
		"default.timepoint_title":   "優しいリマインダー",
		"default.timepoint_message": "時間です — 立ち上がって歩き、水を飲み、遠くを見ましょう。",

		"manual.msg":                 "これはトレイをクリックして表示したリマインダーです。",
		"pomodoro.break_start_title": "🍅 休憩時間です！",
		"pomodoro.break_end_title":   "🍅 休憩終了",

		"about.title": "PomodoroNotifier について",
		"about.body":  "PomodoroNotifier 1.0\n\n• ポモドーロループ + 定時リマインダー\n• ランダム名言（オンライン + オフライン兜底）\n• WebView2 精美ポップアップ\n\n左クリックトレイ = 今すぐ表示\n右クリックトレイ = メニュー",

		"weather.clear":             "晴れ",
		"weather.mainly_clear":      "大体晴れ",
		"weather.partly_cloudy":     "一部曇り",
		"weather.overcast":          "曇り",
		"weather.fog":               "霧",
		"weather.drizzle":           "霧雨",
		"weather.freezing_drizzle":  "着氷霧雨",
		"weather.rain":              "雨",
		"weather.freezing_rain":     "着氷雨",
		"weather.snow":              "雪",
		"weather.snow_grains":       "雪あられ",
		"weather.rain_showers":      "にわか雨",
		"weather.snow_showers":      "にわか雪",
		"weather.thunderstorm":      "雷雲",
		"weather.thunderstorm_hail": "ひょうを伴う雷雲",
		"weather.unknown":           "不明",
	},

	Ko: {
		"tray.tooltip.normal":  "🍅 PomodoroNotifier",
		"tray.tooltip.idle":    "🍅 PomodoroNotifier - 대기 중",
		"tray.tooltip.work":    "🍅 PomodoroNotifier - 집중 중",
		"tray.tooltip.break":   "🍅 PomodoroNotifier - 휴식 중",
		"tray.tooltip.paused":  "🍅 PomodoroNotifier - 일시정지",
		"tray.tooltip.dnd":     "🌙 PomodoroNotifier - 방해금지(회의)",
		"tray.menu.popup":      "지금 표시",
		"tray.menu.settings":   "설정…",
		"tray.menu.pause":      "일정 일시정지(토글)",
		"tray.menu.dnd":        "방해금지 모드(회의)",
		"tray.menu.skip":       "현재 휴식 건너뛰기",
		"tray.menu.extend":     "휴식 5분 연장",
		"tray.menu.sound":      "소리",
		"tray.menu.autostart":  "부팅 시 시작",
		"tray.menu.reload":     "설정 다시 불러오기",
		"tray.menu.opendir":    "설정 폴더 열기",
		"tray.menu.about":      "정보",
		"tray.menu.exit":       "종료",
		"tray.notify.dnd_on":   "방해금지 시작: 팝업 일시정지, 포모도로 계속 진행",
		"tray.notify.dnd_off":  "방해금지 종료",
		"tray.notify.reloaded": "설정 다시 불러옴",
		"tray.status.work":     "🍅 집중 중 · %s 남음 · 오늘 %d🍅",
		"tray.status.break":    "☕ 휴식 중 · %s 남음 · 오늘 %d🍅",
		"tray.status.idle":     "🍅 대기 중 · 오늘 %d🍅",

		"info.skip.title":      "휴식 건너뛰기",
		"info.skip.msg":        "현재 휴식 단계가 아니므로 건너뛸 수 없습니다.",
		"info.extend.title":    "휴식 연장",
		"info.extend.msg":      "현재 휴식 단계가 아니므로 연장할 수 없습니다.",
		"info.sound.title":     "소리",
		"info.sound.msg":       "저장 실패: ",
		"info.autostart.title": "자동 시작",
		"info.autostart.msg":   "설정 실패: ",
		"info.reload.title":    "다시 불러오기 실패",
		"info.webview.title":   "UI 오류",
		"info.webview.msg":     "팝업 렌더 실패, 시스템 대화상자로 대체했습니다.\nWebView2 Runtime이 없을 수 있습니다. 설치 후 재시작하세요.",
		"fail.title":           "PomodoroNotifier 시작 실패",
		"cfgerror.title":       "설정 오류",
		"cfgerror.msg":         "설정 파일에 오류가 있어 기본 설정을 사용합니다.\n확인하세요: ",

		"popup.badge.pomodoro_break_start": "휴식 알림",
		"popup.badge.pomodoro_break_end":   "집중 시작",
		"popup.badge.timepoint":            "예약 알림",
		"popup.badge.manual":               "수동 알림",
		"popup.badge.test":                 "테스트",
		"popup.badge.default":              "알림",
		"popup.title_fallback":             "알림",
		"popup.msg_fallback":               "시간입니다. 일어나서 몸을 펴세요.",
		"popup.weather_loading":            "날씨 불러오는 중…",
		"popup.weather_error":              "날씨 가져오기 실패",
		"popup.snooze_label":               "나중에 알림",
		"popup.snooze_5":                   "5분",
		"popup.snooze_10":                  "10분",
		"popup.snooze_15":                  "15분",
		"popup.auto_close":                 "초 후 자동 닫힘",
		"popup.close":                      "확인",
		"popup.data_error":                 "팝업 데이터 파싱 오류: ",

		"settings.title":               "⚙️ PomodoroNotifier 설정",
		"settings.language":            "표시 언어",
		"settings.language.hint":       "UI 언어. 알림 내용과 명언은 그대로 둡니다.",
		"settings.sec.pomodoro":        "포모도로",
		"settings.pomodoro.enabled":    "포모도로 순환 사용",
		"settings.pomodoro.work":       "작업",
		"settings.pomodoro.minutes":    "분",
		"settings.pomodoro.break":      "휴식",
		"settings.pomodoro.presets":    "빠른 프리셋",
		"settings.sec.timepoint":       "시간대 알림",
		"settings.timepoint.enabled":   "예약 알림 사용",
		"settings.timepoint.add":       "+ 시간대 추가",
		"settings.timepoint.hint":      "시간 형식 HH:MM; 제목/내용을 비우면 아래 기본값 사용.",
		"settings.sec.sound":           "알림음",
		"settings.sound.enabled":       "팝업 시 알림음 재생",
		"settings.sec.weather":         "날씨(팝업 내)",
		"settings.weather.enabled":     "팝업에 날씨 표시",
		"settings.weather.city":        "도시",
		"settings.weather.placeholder": "예: 베이징 / 상하이",
		"settings.weather.hint":        "Open-Meteo 사용(무료, 키 불필요). 오프라인 시 자동 숨김.",
		"settings.sec.popup_quote":     "팝업 및 명언",
		"settings.popup.position":      "팝업 위치",
		"settings.pos.center":          "중앙",
		"settings.pos.topleft":         "좌상",
		"settings.pos.topright":        "우상",
		"settings.pos.bottomleft":      "좌하",
		"settings.pos.bottomright":     "우하",
		"settings.quote.api":           "명언 API",
		"settings.quote.timeout":       "시간 초과",
		"settings.sec.stats":           "통계",
		"settings.stats.today":         "오늘 완료",
		"settings.stats.pomodoros":     "포모도로",
		"settings.stats.last7":         "최근 7일",
		"settings.sec.other":           "기타",
		"settings.other.autostart":     "부팅 시 시작",
		"settings.btn.cancel":          "취소",
		"settings.btn.save":            "저장",
		"settings.err.parse_cfg":       "설정 파싱 오류: ",
		"settings.err.parse_form":      "폼 파싱 오류: ",
		"settings.err.minutes":         "작업/휴식 분은 0보다 커야 합니다",
		"settings.err.timepoint_fmt":   "시간대 형식 오류(HH:MM 여야 함): ",
		"settings.err.timeout_fmt":     "명언 시간초과 형식 오류(예 1500ms): ",
		"settings.err.save":            "저장 실패: ",

		"pomodoro.work_prefix": "%d분 일하고, %d분 휴식. ",

		"default.pomodoro_work":     "휴식 시간! 일어나서 몸을 펴고 먼 곳을 바라보세요.",
		"default.pomodoro_break":    "휴식 종료, 다음 포모도로를 시작하세요.",
		"default.timepoint_title":   "친절한 알림",
		"default.timepoint_message": "시간이 됐어요 — 일어나 걷고, 물을 마시고, 먼 곳을 보세요.",

		"manual.msg":                 "이것은 트레이를 클릭해 표시한 알림입니다.",
		"pomodoro.break_start_title": "🍅 휴식 시간!",
		"pomodoro.break_end_title":   "🍅 휴식 종료",

		"about.title": "PomodoroNotifier 정보",
		"about.body":  "PomodoroNotifier 1.0\n\n• 포모도로 순환 + 예약 알림\n• 무작위 명언(온라인 + 오프라인 대비)\n• WebView2 정교한 팝업\n\n왼쪽 클릭 트레이 = 지금 표시\n오른쪽 클릭 트레이 = 메뉴",

		"weather.clear":             "맑음",
		"weather.mainly_clear":      "대체로 맑음",
		"weather.partly_cloudy":     "부분적 흐림",
		"weather.overcast":          "흐림",
		"weather.fog":               "안개",
		"weather.drizzle":           "이슬비",
		"weather.freezing_drizzle":  "어는 이슬비",
		"weather.rain":              "비",
		"weather.freezing_rain":     "어는 비",
		"weather.snow":              "눈",
		"weather.snow_grains":       "싸락눈",
		"weather.rain_showers":      "소나기",
		"weather.snow_showers":      "눈소나기",
		"weather.thunderstorm":      "뇌우",
		"weather.thunderstorm_hail": "우박 동반 뇌우",
		"weather.unknown":           "알 수 없음",
	},
}
