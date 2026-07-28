// walk.go：目录遍历器。
// 代码注释使用中文。
package media

import (
	"context"
	"io/fs"
	"path/filepath"
)

// Walk 递归遍历 root，返回受支持图片/视频文件。
func Walk(ctx context.Context, root string) ([]FileEntry, error) {
	var out []FileEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		kind, ok := SupportedKind(path)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, FileEntry{
			AbsPath:      path,
			RelativePath: NormalizeRelPath(rel),
			Kind:         kind,
			Size:         info.Size(),
			Mtime:        info.ModTime().UTC(),
		})
		return nil
	})
	return out, err
}
