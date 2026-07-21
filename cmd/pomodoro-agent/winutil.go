package main

import (
	"fmt"
	"os"
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

// setAutostart 通过 HKCU\Software\Microsoft\Windows\CurrentVersion\Run
// 设置/取消开机自启（不引入额外依赖，直接用 advapi32 的注册表 API）。
func setAutostart(enabled bool) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	const (
		HKEY_CURRENT_USER = 0x80000001
		KEY_SET_VALUE     = 0x0002
		REG_SZ            = 1
	)
	adv := syscall.NewLazyDLL("advapi32.dll")
	procOpen := adv.NewProc("RegOpenKeyExW")
	procSet := adv.NewProc("RegSetValueExW")
	procDel := adv.NewProc("RegDeleteValueW")
	procClose := adv.NewProc("RegCloseKey")

	keyPath := `Software\Microsoft\Windows\CurrentVersion\Run`
	pPath, err := syscall.UTF16PtrFromString(keyPath)
	if err != nil {
		return err
	}
	var hkey syscall.Handle
	r, _, _ := procOpen.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(pPath)), 0, KEY_SET_VALUE, uintptr(unsafe.Pointer(&hkey)))
	if r != 0 {
		return fmt.Errorf("RegOpenKeyEx failed: %d", r)
	}
	defer procClose.Call(uintptr(hkey))

	appName := "PomodoroNotifier"
	pName, err := syscall.UTF16PtrFromString(appName)
	if err != nil {
		return err
	}
	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		val, err := syscall.UTF16PtrFromString(exe)
		if err != nil {
			return err
		}
		// REG_SZ: (hkey, name, reserved=0, type, data, datasize in bytes incl. null terminator)
		r, _, _ = procSet.Call(uintptr(hkey), uintptr(unsafe.Pointer(pName)), 0, REG_SZ, uintptr(unsafe.Pointer(val)), uintptr((len(exe)+1)*2))
		if r != 0 {
			return fmt.Errorf("RegSetValueEx failed: %d", r)
		}
	} else {
		procDel.Call(uintptr(hkey), uintptr(unsafe.Pointer(pName)))
	}
	return nil
}
