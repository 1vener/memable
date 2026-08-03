// 包 cmdx：子进程命令封装。Windows 上统一设置 HideWindow，
// 避免 GUI 宿主（windowsgui 构建的 server）启动 ffmpeg/ffprobe/PowerShell
// 等控制台程序时弹出黑色控制台窗口。
// 代码注释使用中文。
package cmdx

import (
	"context"
	"os/exec"
)

// Command 创建子进程命令（带上下文），Windows 上自动隐藏控制台窗口。
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	return cmd
}

// CommandNoCtx 创建子进程命令（无上下文），Windows 上自动隐藏控制台窗口。
func CommandNoCtx(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	return cmd
}
