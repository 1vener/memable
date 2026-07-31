// recycle_test.go：回收站删除策略测试。
// 代码注释使用中文。
package recycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestToBinDir 验证整目录移入回收站（Windows 需 PowerShell，否则跳过）。
func TestToBinDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("powershell"); err != nil {
			t.Skip("powershell 未安装，跳过回收站测试")
		}
	}
	if err := ToBinDir(dir); err != nil {
		t.Fatalf("ToBinDir: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("目录应已移入回收站: %v", err)
	}
}
