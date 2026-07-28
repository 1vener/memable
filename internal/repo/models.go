// 包 repo：数据模型与各表 Repository。
// 代码注释使用中文。
package repo

import "time"

// Library 收藏库（媒体根目录）。
type Library struct {
	ID        int64
	Name      string
	Path      string // 根目录绝对路径
	Kind      string // image/video/mixed
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ScanSession 扫描会话；IsTemporary=1 表示临时扫描。
type ScanSession struct {
	ID          string // UUID
	LibraryID   *int64 // NULL 表示独立临时扫描
	IsTemporary bool
	Status      string // running/completed/failed/cancelled/promoted
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// Media 媒体文件（图片/视频统一）。
type Media struct {
	ID            int64
	LibraryID     int64
	ScanSessionID *string
	Kind          string // image/video
	RelativePath  string // 相对 library.path
	FileSize      int64
	Mtime         time.Time
	Format        *string
	Width         *int
	Height        *int
	Phash         *string
	Dhash         *string
	Ahash         *string
	DurationMs    *int64
	VideoCodec    *string
	AudioCodec    *string
	FrameRate     *float64
	BitRate       *int64
	Oshash        *string
	Sha1          *string
	ThumbnailPath *string
	CreatedAt     time.Time
}

