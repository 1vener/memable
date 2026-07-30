// walk.go：目录遍历器。
// 代码注释使用中文。
package media

import (
	"context"
	"io/fs"
	"path/filepath"
)

// WalkResult 遍历结果。
type WalkResult struct {
	Entries     []FileEntry
	SkippedGIF  int
	Unsupported int
}

// Walk 递归遍历 root，返回受支持文件，跳过已知但不需要处理的格式（如 GIF）。
func Walk(ctx context.Context, root string) WalkResult {
	var out []FileEntry
	var skippedGIF, unsupported int
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		format, ok := SupportedFormat(path)
		if !ok {
			unsupported++
			return nil
		}
		// 已知但按产品要求跳过的格式
		if format.Decoder == DecoderSkip {
			skippedGIF++
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
			Kind:         format.Kind,
			Decoder:      format.Decoder,
			Size:         info.Size(),
			Mtime:        info.ModTime().UTC(),
		})
		return nil
	})
	return WalkResult{Entries: out, SkippedGIF: skippedGIF, Unsupported: unsupported}
}
