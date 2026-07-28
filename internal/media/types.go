// types.go：媒体类型识别与路径规范化。
// 代码注释使用中文。
package media

import (
	"path/filepath"
	"strings"
	"time"
)

// Kind 媒体类型。
type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
)

// FileEntry 遍历产出的文件条目。
type FileEntry struct {
	AbsPath      string
	RelativePath string
	Kind         Kind
	Size         int64
	Mtime        time.Time
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
}
var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
}

// SupportedKind 根据扩展名返回媒体类型；不支持则 ok=false。
func SupportedKind(path string) (Kind, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if imageExts[ext] {
		return KindImage, true
	}
	if videoExts[ext] {
		return KindVideo, true
	}
	return "", false
}

// NormalizeRelPath 将路径分隔符统一为正斜杠，便于跨平台一致性。
func NormalizeRelPath(p string) string {
	return filepath.ToSlash(p)
}
