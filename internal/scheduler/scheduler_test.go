package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"pomodoro-notifier/internal/config"
)

func captureEmitter() (func(PopupEvent), *[]PopupEvent) {
	var events []PopupEvent
	return func(e PopupEvent) { events = append(events, e) }, &events
}

func baseConfig() config.AppConfig {
	cfg := config.DefaultConfig()
	cfg.Pomodoro.WorkStart = "00:00"
	cfg.Pomodoro.WorkEnd = "23:59"
	return cfg
}

// 同一分钟内，无论 tick 多少次，时间点只触发一次（修复“刷屏”）。
func TestTimepointDedupOncePerMinute(t *testing.T) {
	now := time.Date(2026, 7, 20, 10, 30, 5, 0, time.Local)
	cfg := baseConfig()
	cfg.Timepoint = config.TimepointConfig{
		Enabled: true,
		Times:   []config.TimepointItem{{Time: "10:30"}},
		Title:   "提醒",
		Message: "起来走走",
	}
	emit, events := captureEmitter()
	s := New(cfg, time.Local, emit)
	for i := 0; i < 5; i++ {
		s.Tick(now)
	}
	if len(*events) != 1 {
		t.Fatalf("同一分钟内应只触发 1 次，实际 %d 次: %+v", len(*events), *events)
	}
}

// 番茄钟相位：work -> break_start -> break -> break_end -> work。
func TestPomodoroPhaseTransitions(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 9, 0, 0, 0, time.Local)
	wd := int(t0.Weekday())
	if wd == 0 {
		wd = 7
	}
	cfg := baseConfig()
	cfg.Pomodoro.Enabled = true
	cfg.Pomodoro.WorkMinutes = 1
	cfg.Pomodoro.BreakMinutes = 1
	cfg.Pomodoro.WorkDays = []int{wd}
	emit, events := captureEmitter()
	s := New(cfg, time.Local, emit)

	s.Tick(t0) // 初始化 work
	if s.State() != StateWork {
		t.Fatalf("初始应为 work，实际 %s", s.State())
	}
	s.Tick(t0.Add(30 * time.Second))
	if s.State() != StateWork {
		t.Fatalf("30s 后应为 work")
	}
	s.Tick(t0.Add(61 * time.Second)) // 触发 break_start
	if s.State() != StateBreak {
		t.Fatalf("61s 后应为 break，实际 %s", s.State())
	}
	s.Tick(t0.Add(91 * time.Second))
	if s.State() != StateBreak {
		t.Fatalf("91s 后应为 break")
	}
	s.Tick(t0.Add(122 * time.Second)) // 触发 break_end -> work
	if s.State() != StateWork {
		t.Fatalf("122s 后应为 work，实际 %s", s.State())
	}
	var kinds []string
	for _, e := range *events {
		kinds = append(kinds, e.Kind)
	}
	want := []string{"pomodoro_break_start", "pomodoro_break_end"}
	if len(kinds) != 2 || kinds[0] != want[0] || kinds[1] != want[1] {
		t.Fatalf("事件应为 %v，实际 %v", want, kinds)
	}
}

// 时间点按配置时区触发。
func TestTimepointTimezone(t *testing.T) {
	loc := time.FixedZone("TEST+8", 8*3600)
	now := time.Date(2026, 7, 20, 10, 30, 0, 0, loc)
	cfg := baseConfig()
	cfg.Timepoint = config.TimepointConfig{
		Enabled: true,
		Times:   []config.TimepointItem{{Time: "10:30"}},
	}
	emit, events := captureEmitter()
	s := New(cfg, loc, emit)
	s.Tick(now)
	if len(*events) != 1 {
		t.Fatalf("时区下 10:30 应触发，实际 %d", len(*events))
	}

	emit2, events2 := captureEmitter()
	s2 := New(cfg, loc, emit2)
	s2.Tick(time.Date(2026, 7, 20, 11, 0, 0, 0, loc))
	if len(*events2) != 0 {
		t.Fatalf("11:00 不应触发，实际 %d", len(*events2))
	}
}

// 配置热重载：启用状态下保留进行中的番茄钟；关闭时才清空。
func TestUpdateConfigPreservesPomodoro(t *testing.T) {
	t0 := time.Date(2026, 7, 20, 9, 0, 0, 0, time.Local)
	wd := int(t0.Weekday())
	if wd == 0 {
		wd = 7
	}
	cfg := baseConfig()
	cfg.Pomodoro.Enabled = true
	cfg.Pomodoro.WorkMinutes = 25
	cfg.Pomodoro.WorkDays = []int{wd}
	emit, _ := captureEmitter()
	s := New(cfg, time.Local, emit)
	s.Tick(t0)
	if !s.pomo.active || s.pomo.phase != "work" {
		t.Fatalf("番茄钟应已进入 work")
	}

	cfg2 := cfg
	cfg2.Pomodoro.WorkMinutes = 30
	s.UpdateConfig(cfg2, time.Local)
	if !s.pomo.active || s.pomo.phase != "work" {
		t.Fatalf("热重载后番茄钟不应被重置: active=%v phase=%s", s.pomo.active, s.pomo.phase)
	}

	cfg3 := cfg2
	cfg3.Pomodoro.Enabled = false
	s.UpdateConfig(cfg3, time.Local)
	if s.pomo.active {
		t.Fatalf("关闭番茄钟后 pomo 应被清空")
	}
}

// 旧版 []string 时间点配置可向后兼容解析为对象数组。
func TestTimepointStringBackwardCompat(t *testing.T) {
	jsonStr := `{"enabled":true,"times":["10:30","14:30"],"title":"T","message":"M"}`
	var tp config.TimepointConfig
	if err := json.Unmarshal([]byte(jsonStr), &tp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(tp.Times) != 2 || tp.Times[0].Time != "10:30" || tp.Times[1].Time != "14:30" {
		t.Fatalf("旧格式应解析为两项: %+v", tp.Times)
	}
}
