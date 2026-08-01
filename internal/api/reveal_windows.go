//go:build windows

// reveal_windows.go：Windows 资源管理器"打开目录并选中文件"（ShellExecuteW）。
// 代码注释使用中文。
package api

import (
	"fmt"
	"syscall"
	"unsafe"
)

// revealInExplorer 用 ShellExecuteW 启动资源管理器并选中指定文件。
// 参数使用 /select,"<path>" 形式（引号只包路径）——这是 explorer 唯一可靠的
// 参数形式；os/exec 会把含空格的参数整体加引号（形如 "/select,C:\path with
// space\f.jpg"），explorer 无法识别该形式，表现为只打开目录而不选中文件。
func revealInExplorer(absPath string) error {
	dll, err := syscall.LoadDLL("shell32.dll")
	if err != nil {
		return fmt.Errorf("加载 shell32.dll: %w", err)
	}
	proc, err := dll.FindProc("ShellExecuteW")
	if err != nil {
		return fmt.Errorf("查找 ShellExecuteW: %w", err)
	}
	op, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString("explorer.exe")
	args, _ := syscall.UTF16PtrFromString(selectArgs(absPath))
	r1, _, _ := proc.Call(
		0, // hwnd（无父窗口）
		uintptr(unsafe.Pointer(op)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(args)),
		0,          // lpDirectory
		uintptr(1), // nShowCmd = SW_SHOWNORMAL
	)
	if int(r1) <= 32 {
		return fmt.Errorf("ShellExecuteW 打开资源管理器失败 (code=%d)", r1)
	}
	return nil
}
