// 包 media：媒体文件识别、遍历与 metadata 采集。
// 代码注释使用中文。
package media

import (
	"path/filepath"
	"strings"
	"time"
)

const (
	KindImage = "image"
	KindVideo = "video"
)

// FileEntry 表示遍历到的受支持媒体文件。
type FileEntry struct {
	AbsPath      string
	RelativePath string
	Kind         string
	Size         int64
	Mtime        time.Time
}

// SupportedKind 根据扩展名判断是否为受支持媒体类型。
func SupportedKind(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		return KindImage, true
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v":
		return KindVideo, true
	default:
		return "", false
	}
}

// NormalizeRelPath 将系统路径分隔符统一为 /，便于跨平台存储与搜索。
func NormalizeRelPath(rel string) string {
	return filepath.ToSlash(filepath.Clean(rel))
}
