// types.go：媒体类型识别、解码策略与路径规范化。
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

// DecoderKind 解码方式。
type DecoderKind string

const (
	DecoderGo     DecoderKind = "go"     // 使用 Go 标准/扩展库直接解码
	DecoderFFmpeg DecoderKind = "ffmpeg" // 需先由 FFmpeg 转换为临时图片再解码
	DecoderSkip   DecoderKind = "skip"   // 已知格式但按产品要求跳过
)

// MediaFormat 单个扩展名的完整处理策略。
type MediaFormat struct {
	Kind       Kind
	Decoder    DecoderKind
	SkipReason string // 仅在 DecoderSkip 时提供原因
}

// supportedFormats 白名单格式表（全小写，含点号）。
var supportedFormats = map[string]MediaFormat{
	// ===== 图片 / Go 原生解码 =====
	".jpg":  {Kind: KindImage, Decoder: DecoderGo},
	".jpeg": {Kind: KindImage, Decoder: DecoderGo},
	".jfif": {Kind: KindImage, Decoder: DecoderGo},
	".png":  {Kind: KindImage, Decoder: DecoderGo},
	".bmp":  {Kind: KindImage, Decoder: DecoderGo},

	// ===== 图片 / 已知但跳过 =====
	".gif": {
		Kind:       KindImage,
		Decoder:    DecoderSkip,
		SkipReason: "GIF 按配置跳过处理",
	},

	// ===== 图片 / 需 FFmpeg 转码 =====
	".heic": {Kind: KindImage, Decoder: DecoderFFmpeg},
	".cr2":  {Kind: KindImage, Decoder: DecoderFFmpeg},

	// ===== 视频 / FFmpeg 处理 =====
	".mp4":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".mov":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".avi":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".mpg":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".mpeg": {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".m4v":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".webm": {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".wmv":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".flv":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".3gp":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".ts":   {Kind: KindVideo, Decoder: DecoderFFmpeg},
	".mkv":  {Kind: KindVideo, Decoder: DecoderFFmpeg},
}

// SupportedFormat 根据扩展名返回完整格式信息；不支持则 ok=false。
func SupportedFormat(path string) (MediaFormat, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	f, ok := supportedFormats[ext]
	return f, ok
}

// SupportedKind 根据扩展名返回媒体类型；不支持则 ok=false。
// 兼容旧调用方，内部转为 SupportedFormat。
func SupportedKind(path string) (Kind, bool) {
	f, ok := SupportedFormat(path)
	if !ok {
		return "", false
	}
	return f.Kind, true
}

// FileEntry 遍历产出的文件条目。
type FileEntry struct {
	AbsPath      string
	RelativePath string
	Kind         Kind
	Decoder      DecoderKind
	Size         int64
	Mtime        time.Time
}

// NormalizeRelPath 将路径分隔符统一为正斜杠，便于跨平台一致性。
func NormalizeRelPath(p string) string {
	return filepath.ToSlash(p)
}
