package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	mu      sync.Mutex
	path    string
	bomOnce sync.Once
}

func New(path string) *Logger {
	return &Logger{path: path}
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(l.path), 0o755)
	l.bomOnce.Do(func() { l.ensureUTF8BOM() })

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

func (l *Logger) ensureUTF8BOM() {
	b, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(l.path, utf8BOM, 0o644)
		}
		return
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return
	}
	nb := make([]byte, 0, len(utf8BOM)+len(b))
	nb = append(nb, utf8BOM...)
	nb = append(nb, b...)
	_ = os.WriteFile(l.path, nb, 0o644)
}
