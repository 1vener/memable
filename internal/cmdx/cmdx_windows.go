//go:build windows

// cmdx_windows.go：Windows 平台隐藏子进程控制台窗口。
// 代码注释使用中文。
package cmdx

import (
	"os/exec"
	"syscall"
)

// hideWindow 设置 CREATE_NO_WINDOW：子进程不创建新控制台窗口。
// 父进程为 GUI 程序（windowsgui 构建）时，console 子进程默认会弹黑窗口。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
