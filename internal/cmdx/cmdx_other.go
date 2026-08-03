//go:build !windows

// cmdx_other.go：非 Windows 平台无需隐藏窗口。
// 代码注释使用中文。
package cmdx

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
