package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"
)

// openInExplorer 打开指定目录（Windows 用 explorer，Mac/Linux 用 xdg-open/open）。
func openInExplorer(dir string) {
	switch runtime.GOOS {
	case "windows":
		// explorer.exe 直接接收路径即可
		_ = exec.Command("explorer", dir).Start()
	case "darwin":
		_ = exec.Command("open", dir).Start()
	default:
		_ = exec.Command("xdg-open", dir).Start()
	}
}

func showAbout() {
	showInfo("关于 PomodoroNotifier",
		"PomodoroNotifier 1.0\n\n"+
			"• 番茄钟循环 + 指定时间点提醒\n"+
			"• 随机诗词/名言（在线 + 离线兜底）\n"+
			"• WebView2 精美弹窗\n\n"+
			"左键托盘 = 立即弹一次\n"+
			"右键托盘 = 菜单")
}

func showError(title, msg string) { showInfo(title, msg) }

// showInfo 调 Windows MessageBox 显示信息。
func showInfo(title, msg string) {
	if runtime.GOOS != "windows" {
		fmt.Println(title, msg)
		return
	}
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(msg))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		0x40, // MB_ICONINFORMATION
	)
}

// win32 MessageBoxW，避免引入额外依赖
var (
	modUser32               = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW         = modUser32.NewProc("MessageBoxW")
)

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		// 不会发生
		return nil
	}
	return p
}
