package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"pomodoro-notifier/internal/config"
)

// PopupEvent 是要展示给用户的弹窗内容（不再走文件事件队列）。
type PopupEvent struct {
	Kind    string  // pomodoro_break_start / pomodoro_break_end / timepoint / manual
	Title   string
	Message string
	At      time.Time
}

type Emitter func(PopupEvent)

// TickDebugf 在每个 timepoint 关键节点打印一行日志（便于定位"到点不弹"问题）。
// 默认空，由 main 包注入。
var TickDebugf = func(format string, args ...any) {}

// State 表示调度器当前所处的视觉状态。
type State string

const (
	StateIdle   State = "idle"   // 暂停 / 调度未启动
	StateWork   State = "work"   // 工作中（番茄钟 work 阶段）
	StateBreak  State = "break"  // 休息中（番茄钟 break 阶段）
)

// ServiceScheduler 是单进程内的调度器。
// 线程安全：Tick 与 UpdateConfig 可并发调用。
type ServiceScheduler struct {
	mu      sync.Mutex
	cfg     config.AppConfig
	loc     *time.Location
	emit    Emitter
	pomo    pomodoroState
	lastT   map[string]map[string]bool // dayKey -> "HH:MM" -> already fired
	lastLog map[string]bool            // dayKey+"HH:MM" -> already logged "already fired" today
	state   State
	onState func(State) // 状态变更回调（必须非阻塞）
}

type pomodoroState struct {
	active      bool
	phase       string // "work" / "break"
	nextAt      time.Time
	lastWorkDay string
}

func New(cfg config.AppConfig, loc *time.Location, emit Emitter) *ServiceScheduler {
	if loc == nil {
		loc = time.Local
	}
	return &ServiceScheduler{
		cfg:     cfg,
		loc:     loc,
		emit:    emit,
		lastT:   map[string]map[string]bool{},
		lastLog: map[string]bool{},
	}
}

func (s *ServiceScheduler) UpdateConfig(cfg config.AppConfig, loc *time.Location) {
	s.mu.Lock()
	s.cfg = cfg
	if loc != nil {
		s.loc = loc
	}
	// 配置改变后清空当天的“已触发”标记，这样新的时间点列表可以立即生效。
	s.lastT = map[string]map[string]bool{}
	s.lastLog = map[string]bool{}
	// 仅在番茄钟被关闭时才清空进行中的番茄钟；否则保留当前相位/nextAt，
	// 避免用户改配置（如调整时间点）时正在计时的番茄钟被重置。
	if !cfg.Pomodoro.Enabled {
		s.pomo = pomodoroState{}
	}
	emit := s.emit
	now := time.Now().In(s.loc)
	dayKey := now.Format("2006-01-02")
	s.lastT[dayKey] = map[string]bool{}
	// 新加的“当前分钟 = HH:MM”的时间点立即触发一次（方便用户改完立刻验证）
	// 注意：tickTimepoints 也会处理，但本分钟内 tick 还没到；这里直接补一发。
	if emit != nil {
		s.fireImmediateTimepointsLocked(cfg.Timepoint, now, dayKey, emit)
	}
	s.mu.Unlock()
}

// fireImmediateTimepointsLocked 必须在持有 mu 时调用。
// 仅对“当前分钟 == HH:MM 且本分钟内 lastT 未标记”的项发一次，
// 然后把这一分钟的 lastT 标记掉，避免 tickTimepoints 再发一次。
func (s *ServiceScheduler) fireImmediateTimepointsLocked(tp config.TimepointConfig, now time.Time, dayKey string, emit Emitter) {
	if !tp.Enabled || emit == nil {
		return
	}
	for _, it := range tp.Times {
		h, m, err := parseHM(it.Time)
		if err != nil {
			continue
		}
		if now.Hour() != h || now.Minute() != m {
			continue
		}
		minKey := fmt.Sprintf("%02d:%02d", h, m)
		if s.lastT[dayKey][minKey] {
			continue
		}
		s.lastT[dayKey][minKey] = true
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = strings.TrimSpace(tp.Title)
		}
		if title == "" {
			title = "温馨提醒"
		}
		msg := strings.TrimSpace(it.Message)
		if msg == "" {
			msg = strings.TrimSpace(tp.Message)
		}
		if msg == "" {
			msg = "到点啦，起来走走。"
		}
		emit(PopupEvent{Kind: "timepoint", Title: title, Message: msg, At: now})
	}
}

// Pause 暂停调度（清空番茄钟状态，停止再触发，但配置不变）。
func (s *ServiceScheduler) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pomo = pomodoroState{}
	s.lastT = map[string]map[string]bool{}
	s.lastLog = map[string]bool{}
	s.setStateLocked(StateIdle)
}

// SetStateListener 注册状态变更回调（在状态变化时同步调用，回调内不要阻塞）。
func (s *ServiceScheduler) SetStateListener(fn func(State)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onState = fn
}

// State 返回当前调度状态（线程安全）。
func (s *ServiceScheduler) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetEmitter 替换事件回调（用于先创建 scheduler 再注入 emit 的场景）。
func (s *ServiceScheduler) SetEmitter(emit Emitter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit = emit
}

// CurrentConfig 返回当前生效的配置（线程安全）。
// emit 等回调应从这里取最新配置，而不是捕获启动时的 cfg 副本，
// 否则配置热重载不会反映在弹窗/调度行为上。
func (s *ServiceScheduler) CurrentConfig() config.AppConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// setStateLocked 必须在持有 mu 时调用。
func (s *ServiceScheduler) setStateLocked(st State) {
	if s.state == st {
		return
	}
	s.state = st
	fn := s.onState
	if fn != nil {
		fn(st)
	}
}

// Tick 推进一次调度。now 任意时刻都可传入，内部按 loc 归一化。
func (s *ServiceScheduler) Tick(now time.Time) {
	s.mu.Lock()
	cfg := s.cfg
	loc := s.loc
	emit := s.emit
	s.mu.Unlock()

	now = now.In(loc)
	s.tickTimepoints(cfg, now, emit)
	s.tickPomodoro(cfg, now, emit)
}

func (s *ServiceScheduler) tickPomodoro(cfg config.AppConfig, now time.Time, emit Emitter) {
	if !cfg.Pomodoro.Enabled || emit == nil {
		s.mu.Lock()
		s.pomo = pomodoroState{}
		s.setStateLocked(StateIdle)
		s.mu.Unlock()
		return
	}
	if !inWorkday(now, cfg.Pomodoro.WorkDays) || !inWorkHours(now, cfg.Pomodoro.WorkStart, cfg.Pomodoro.WorkEnd) {
		s.mu.Lock()
		s.pomo = pomodoroState{}
		s.setStateLocked(StateIdle)
		s.mu.Unlock()
		return
	}

	// 关键：先把要 emit 的事件和状态更新收集好，释放锁后再 emit，
	// 否则 emit 里如果再访问 sched（CurrentConfig 等）会与持有 mu 死锁。
	var toFire []PopupEvent
	s.mu.Lock()
	dayKey := now.Format("2006-01-02")
	if s.pomo.lastWorkDay != dayKey {
		s.pomo = pomodoroState{active: true, phase: "work", nextAt: now.Add(time.Duration(cfg.Pomodoro.WorkMinutes) * time.Minute), lastWorkDay: dayKey}
		s.setStateLocked(StateWork)
		s.mu.Unlock()
		return
	}
	if !s.pomo.active || s.pomo.nextAt.IsZero() {
		s.pomo = pomodoroState{active: true, phase: "work", nextAt: now.Add(time.Duration(cfg.Pomodoro.WorkMinutes) * time.Minute), lastWorkDay: dayKey}
		s.setStateLocked(StateWork)
		s.mu.Unlock()
		return
	}
	if now.Before(s.pomo.nextAt) {
		// 在阶段进行中，确保状态正确
		switch s.pomo.phase {
		case "work":
			s.setStateLocked(StateWork)
		case "break":
			s.setStateLocked(StateBreak)
		}
		s.mu.Unlock()
		return
	}

	switch s.pomo.phase {
	case "work":
		title := "🍅 休息时间到！"
		msg := fmt.Sprintf("工作了 %d 分钟，休息 %d 分钟。%s", cfg.Pomodoro.WorkMinutes, cfg.Pomodoro.BreakMinutes, cfg.Pomodoro.BreakText)
		toFire = append(toFire, PopupEvent{Kind: "pomodoro_break_start", Title: title, Message: msg, At: now})
		s.pomo.phase = "break"
		s.pomo.nextAt = now.Add(time.Duration(cfg.Pomodoro.BreakMinutes) * time.Minute)
		s.setStateLocked(StateBreak)
	case "break":
		title := "🍅 休息结束"
		msg := "休息时间到，开始下一个番茄钟！"
		if strings.TrimSpace(cfg.Pomodoro.WorkText) != "" {
			msg = cfg.Pomodoro.WorkText
		}
		toFire = append(toFire, PopupEvent{Kind: "pomodoro_break_end", Title: title, Message: msg, At: now})
		s.pomo.phase = "work"
		s.pomo.nextAt = now.Add(time.Duration(cfg.Pomodoro.WorkMinutes) * time.Minute)
		s.setStateLocked(StateWork)
	}
	s.mu.Unlock()
	for _, e := range toFire {
		emit(e)
	}
}

func (s *ServiceScheduler) tickTimepoints(cfg config.AppConfig, now time.Time, emit Emitter) {
	if !cfg.Timepoint.Enabled || emit == nil {
		return
	}
	dayKey := now.Format("2006-01-02")
	// 关键：在锁内只收集"要 emit 的事件"，释放锁后再调 emit，
	// 否则 emit 里如果再访问 sched（CurrentConfig 等）会与持有 mu 死锁。
	var toFire []PopupEvent
	s.mu.Lock()
	if _, ok := s.lastT[dayKey]; !ok {
		s.lastT[dayKey] = map[string]bool{}
	}
	for _, it := range cfg.Timepoint.Times {
		h, m, err := parseHM(it.Time)
		if err != nil {
			TickDebugf("timepoint parse failed: %q err=%v", it.Time, err)
			continue
		}
		if now.Hour() != h || now.Minute() != m {
			continue
		}
		minKey := fmt.Sprintf("%02d:%02d", h, m)
		// 去重始终基于 (dayKey+minKey)，无论旧配置的 once_per_day 如何，
		// 保证每个时间点每天只在命中的那一分钟内触发一次，不会每秒刷屏。
		if s.lastT[dayKey][minKey] {
			llKey := dayKey + "|" + minKey
			if !s.lastLog[llKey] {
				s.lastLog[llKey] = true
				TickDebugf("timepoint already fired today: %s day=%s", minKey, dayKey)
			}
			continue
		}
		s.lastT[dayKey][minKey] = true
		TickDebugf("timepoint FIRE: %s day=%s", minKey, dayKey)
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = strings.TrimSpace(cfg.Timepoint.Title)
		}
		if title == "" {
			title = "温馨提醒"
		}
		msg := strings.TrimSpace(it.Message)
		if msg == "" {
			msg = strings.TrimSpace(cfg.Timepoint.Message)
		}
		if msg == "" {
			msg = "到点啦，起来走走。"
		}
		toFire = append(toFire, PopupEvent{Kind: "timepoint", Title: title, Message: msg, At: now})
	}
	s.mu.Unlock()
	for _, e := range toFire {
		emit(e)
	}
}

func inWorkday(t time.Time, workDays []int) bool {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	for _, d := range workDays {
		if d == wd {
			return true
		}
	}
	return false
}

func inWorkHours(t time.Time, startHM, endHM string) bool {
	sh, sm, err := parseHM(startHM)
	if err != nil {
		return true
	}
	eh, em, err := parseHM(endHM)
	if err != nil {
		return true
	}
	start := time.Date(t.Year(), t.Month(), t.Day(), sh, sm, 0, 0, t.Location())
	end := time.Date(t.Year(), t.Month(), t.Day(), eh, em, 0, 0, t.Location())
	return !t.Before(start) && t.Before(end)
}

func parseHM(s string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("bad time format")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, errors.New("bad time range")
	}
	return h, m, nil
}
