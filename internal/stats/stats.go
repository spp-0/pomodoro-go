package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DayStat 某一天的统计。
type DayStat struct {
	Pomodoros  int `json:"pomodoros"`  // 完成的番茄钟数
	Timepoints int `json:"timepoints"` // 命中的时间点提醒数
}

// Store 按日期保存统计，文件落盘，自带互斥锁。
// 仅保留最近 90 天，避免文件无限增长。
type Store struct {
	mu   sync.Mutex
	path string
	data map[string]DayStat // "2006-01-02" -> stat
}

const keepDays = 90

// New 载入（若存在）或新建一个统计存储。
func New(path string) *Store {
	s := &Store{path: path, data: map[string]DayStat{}}
	s.load()
	return s
}

func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	if s.data == nil {
		s.data = map[string]DayStat{}
	}
}

// RecordPomodoro 记录一个完成的番茄钟（按本地日期）。
func (s *Store) RecordPomodoro(date string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.data[date]
	d.Pomodoros++
	s.data[date] = d
	s.pruneLocked()
	s.saveLocked()
}

// RecordTimepoint 记录一次命中的时间点提醒。
func (s *Store) RecordTimepoint(date string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.data[date]
	d.Timepoints++
	s.data[date] = d
	s.pruneLocked()
	s.saveLocked()
}

// ForDate 返回指定日期的统计（不存在时为零值）。
func (s *Store) ForDate(date string) DayStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[date]
}

// Today 返回今天（按本地时区）的统计。
func (s *Store) Today() DayStat {
	return s.ForDate(time.Now().Format("2006-01-02"))
}

// Last7 返回最近 7 天（含今天）的统计，按时间正序。
func (s *Store) Last7() []DayStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DayStat, 0, 7)
	today := time.Now()
	for i := 6; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, s.data[d])
	}
	return out
}

func (s *Store) pruneLocked() {
	cutoff := time.Now().AddDate(0, 0, -keepDays).Format("2006-01-02")
	for k := range s.data {
		if k < cutoff {
			delete(s.data, k)
		}
	}
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "stats-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
	}
}
