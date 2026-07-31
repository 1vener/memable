// 包 recycle：本地文件删除安全策略（默认移入系统回收站，可配置永久删除）。
// 代码注释使用中文。
package recycle

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ToBin 将文件移入系统回收站；Windows 使用 PowerShell 调用 Microsoft.VisualBasic，
// 其他平台回退为直接删除（无回收站语义）。
func ToBin(path string) error {
	if runtime.GOOS != "windows" {
		return os.Remove(path)
	}
	escaped := strings.ReplaceAll(path, "'", "''")
	cmd := exec.Command(
		"powershell",
		"-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf(
			"Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile('%s','OnlyErrorDialogs','SendToRecycleBin')",
			escaped,
		),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("移入回收站失败: %s %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ToBinDir 将整个目录（含子目录与文件）移入系统回收站；
// Windows 使用 PowerShell 的 FileSystem.DeleteDirectory(SendToRecycleBin)，
// 其他平台回退为直接删除。
func ToBinDir(path string) error {
	if runtime.GOOS != "windows" {
		return os.RemoveAll(path)
	}
	escaped := strings.ReplaceAll(path, "'", "''")
	cmd := exec.Command(
		"powershell",
		"-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf(
			"Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory('%s','OnlyErrorDialogs','SendToRecycleBin')",
			escaped,
		),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("目录移入回收站失败: %s %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
