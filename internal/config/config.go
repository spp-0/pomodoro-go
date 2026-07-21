package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type QuoteAPIConfig struct {
	URL     string `json:"url"`
	Timeout string `json:"timeout"`
}

type PomodoroConfig struct {
	Enabled      bool   `json:"enabled"`
	WorkMinutes  int    `json:"work_minutes"`
	BreakMinutes int    `json:"break_minutes"`
	WorkDays     []int  `json:"work_days"`
	WorkStart    string `json:"work_start"`
	WorkEnd      string `json:"work_end"`
	WorkText     string `json:"work_text"`
	BreakText    string `json:"break_text"`
}

// TimepointItem 表示单个时间点（可带独立标题与内容）。
type TimepointItem struct {
	Time    string `json:"time"`    // "HH:MM"
	Title   string `json:"title"`   // 缺省回落 TimepointConfig.Title
	Message string `json:"message"` // 缺省回落 TimepointConfig.Message
}

// TimepointConfig 指定时间点提醒配置。
// Times 支持两种 JSON 写法以兼容旧配置：
//   - 字符串数组：["10:30","14:30"]（共用根级 Title/Message）
//   - 对象数组：[{"time":"10:30","title":"喝水","message":"去倒杯水"}]
type TimepointConfig struct {
	Enabled bool            `json:"enabled"`
	Times   []TimepointItem `json:"times"`
	Title   string          `json:"title"`
	Message string          `json:"message"`
}

// UnmarshalJSON 兼容旧版 []string 写法，并统一为 []TimepointItem。
func (t *TimepointConfig) UnmarshalJSON(data []byte) error {
	type alias struct {
		Enabled bool            `json:"enabled"`
		Times   json.RawMessage `json:"times"`
		Title   string          `json:"title"`
		Message string          `json:"message"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	t.Enabled = a.Enabled
	t.Title = a.Title
	t.Message = a.Message
	if len(a.Times) == 0 || string(a.Times) == "null" {
		return nil
	}
	// 先尝试新格式 []TimepointItem
	var items []TimepointItem
	if err := json.Unmarshal(a.Times, &items); err == nil {
		t.Times = items
		return nil
	}
	// 回退旧格式 []string
	var strs []string
	if err := json.Unmarshal(a.Times, &strs); err != nil {
		return fmt.Errorf("timepoint.times 格式错误: %w", err)
	}
	for _, s := range strs {
		t.Times = append(t.Times, TimepointItem{Time: s})
	}
	return nil
}

// SoundConfig 控制提醒音。
type SoundConfig struct {
	Enabled bool   `json:"enabled"` // 是否播放提醒音
	File    string `json:"file"`    // 自定义 wav 路径；空=系统默认提示音
}

type PopupConfig struct {
	AutoCloseSeconds int         `json:"auto_close_seconds"`
	Width            int         `json:"width"`
	Height           int         `json:"height"`
	ClickThrough     bool        `json:"click_through"`
	TopMost          bool        `json:"topmost"`
	Position         string      `json:"position"` // center / top-left / top-right / bottom-left / bottom-right
	Sound            SoundConfig `json:"sound"`
}

// WeatherConfig 控制弹窗内天气信息。
type WeatherConfig struct {
	Enabled bool   `json:"enabled"` // 是否在弹窗内显示天气
	City    string `json:"city"`    // 城市名（中文/英文均可，经 Open-Meteo 地理编码）
}

type AppConfig struct {
	LogFile   string          `json:"log_file"`
	TimeZone  string          `json:"timezone"`
	QuoteAPI  QuoteAPIConfig  `json:"quote_api"`
	Popup     PopupConfig     `json:"popup"`
	Pomodoro  PomodoroConfig  `json:"pomodoro"`
	Timepoint TimepointConfig `json:"timepoint"`
	Weather   WeatherConfig   `json:"weather"`
	Autostart bool            `json:"autostart"`
}

// DefaultConfig 返回默认配置；路径字段需要在调用方按 exe 目录覆盖。
func DefaultConfig() AppConfig {
	return AppConfig{
		LogFile:  "pomodoro.log",
		TimeZone: "",
		QuoteAPI: QuoteAPIConfig{
			URL:     "https://v1.hitokoto.cn/?encode=json",
			Timeout: "1500ms",
		},
		Popup: PopupConfig{
			AutoCloseSeconds: 20,
			Width:            560,
			Height:           340,
			ClickThrough:     false,
			TopMost:          true,
			Position:         "bottom-right",
			Sound:            SoundConfig{Enabled: true},
		},
		Pomodoro: PomodoroConfig{
			Enabled:      true,
			WorkMinutes:  25,
			BreakMinutes: 5,
			WorkDays:     []int{1, 2, 3, 4, 5},
			WorkStart:    "09:00",
			WorkEnd:      "18:00",
			WorkText:     "休息时间到！站起来活动一下、眺望远处。",
			BreakText:    "休息结束，开始下一个番茄钟。",
		},
		Timepoint: TimepointConfig{
			Enabled: true,
			Times: []TimepointItem{
				{Time: "10:30"},
				{Time: "14:30"},
				{Time: "17:30"},
			},
			Title:   "温馨提醒",
			Message: "到点啦，起来走走，喝口水，看看远处。",
		},
		Weather: WeatherConfig{
			Enabled: true,
			City:    "北京",
		},
		Autostart: false,
	}
}

// ApplyDefaults 补齐缺省值。
func (c *AppConfig) ApplyDefaults() {
	if c.LogFile == "" {
		c.LogFile = "pomodoro.log"
	}
	if c.Popup.Width <= 0 {
		c.Popup.Width = 560
	}
	if c.Popup.Height <= 0 {
		c.Popup.Height = 340
	}
	if c.Popup.AutoCloseSeconds <= 0 {
		c.Popup.AutoCloseSeconds = 20
	}
	switch strings.ToLower(strings.TrimSpace(c.Popup.Position)) {
	case "", "center":
		c.Popup.Position = "center"
	case "top-left", "topleft":
		c.Popup.Position = "top-left"
	case "top-right", "topright":
		c.Popup.Position = "top-right"
	case "bottom-left", "bottomleft":
		c.Popup.Position = "bottom-left"
	case "bottom-right", "bottomright":
		c.Popup.Position = "bottom-right"
	default:
		c.Popup.Position = "bottom-right"
	}
	if c.QuoteAPI.URL == "" {
		c.QuoteAPI.URL = "https://v1.hitokoto.cn/?encode=json"
	}
	if c.QuoteAPI.Timeout == "" {
		c.QuoteAPI.Timeout = "1500ms"
	}
	if len(c.Pomodoro.WorkDays) == 0 {
		c.Pomodoro.WorkDays = []int{1, 2, 3, 4, 5}
	}
}

// Load 从 path 读取配置，失败返回错误（调用方负责决定是否生成默认）。
func Load(path string) (AppConfig, error) {
	cfg := DefaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

// Save 把配置写到 path（缩进格式）。
func Save(path string, cfg AppConfig) error {
	cfg.ApplyDefaults()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ExeDir 返回当前 exe 所在目录。
func ExeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// DefaultConfigPath 返回 exe 目录下的 config.json 路径。
func DefaultConfigPath() (string, error) {
	dir, err := ExeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func (c AppConfig) Location() (*time.Location, error) {
	if c.TimeZone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(c.TimeZone)
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (c AppConfig) QuoteTimeout() (time.Duration, error) {
	d, err := time.ParseDuration(c.QuoteAPI.Timeout)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errors.New("quote timeout must be > 0")
	}
	return d, nil
}
