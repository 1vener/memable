//go:build !windows

// reveal_other.go：非 Windows 平台的"打开并选中"占位实现。
// 实际打开逻辑由 openFile 的 darwin/linux 分支完成（open -R / nautilus --select）。
// 代码注释使用中文。
package api

import "fmt"

func revealInExplorer(absPath string) error {
	return fmt.Errorf("当前平台不支持")
}
