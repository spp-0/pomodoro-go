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
	bomOK   bool
}

func New(path string) *Logger {
	return &Logger{path: path}
}

// utf8BOM 是 UTF-8 文件头，Windows 记事本/编辑器靠它识别编码（避免按本地
// 代码页 GBK 解释 UTF-8 字节流，造成中文乱码）。其它现代编辑器（VS Code、
// Notepad++、tail -f）都能正确识别 UTF-8 with/without BOM。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(l.path), 0o755)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	// 仅第一次写入前检查/补 BOM：sync.Once 保证整个进程内只 stat 一次。
	l.bomOnce.Do(func() {
		stat, err := f.Stat()
		if err != nil {
			return
		}
		if stat.Size() == 0 {
			_, _ = f.Write(utf8BOM)
			l.bomOK = true
			return
		}
		var head [3]byte
		if _, err := f.ReadAt(head[:], 0); err == nil &&
			head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF {
			l.bomOK = true
		}
	})

	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
