// 包 repo：数据模型与各表 Repository。
// 代码注释使用中文。
package repo

import "time"

// Library 收藏库（媒体根目录）。
type Library struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"` // 根目录绝对路径
	Kind      string    `json:"kind"` // image/video/mixed
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ScanSession 扫描会话；IsTemporary=1 表示临时扫描。
type ScanSession struct {
	ID          string     `json:"id"`         // UUID
	LibraryID   *int64     `json:"library_id"` // NULL 表示独立临时扫描
	IsTemporary bool       `json:"is_temporary"`
	Status      string     `json:"status"` // running/completed/failed/cancelled/promoted
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

// Media 媒体文件（图片/视频统一）。
type Media struct {
	ID            int64     `json:"id"`
	LibraryID     int64     `json:"library_id"`
	ScanSessionID *string   `json:"scan_session_id"`
	Kind          string    `json:"kind"`          // image/video
	RelativePath  string    `json:"relative_path"` // 相对 library.path
	FileSize      int64     `json:"file_size"`
	Mtime         time.Time `json:"mtime"`
	Format        *string   `json:"format"`
	Width         *int      `json:"width"`
	Height        *int      `json:"height"`
	Phash         *string   `json:"phash"`
	Dhash         *string   `json:"dhash"`
	Ahash         *string   `json:"ahash"`
	DurationMs    *int64    `json:"duration_ms"`
	VideoCodec    *string   `json:"video_codec"`
	AudioCodec    *string   `json:"audio_codec"`
	FrameRate     *float64  `json:"frame_rate"`
	BitRate       *int64    `json:"bit_rate"`
	Oshash        *string   `json:"oshash"`
	Sha1          *string   `json:"sha1"`
	ThumbnailPath *string   `json:"thumbnail_path"`
	CreatedAt     time.Time `json:"created_at"`
}
