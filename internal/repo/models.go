// 包 repo：数据模型与各表 Repository。
// 代码注释使用中文。
package repo

import "time"

// ProgressFunc 进度回调函数（扫描/修复等任务过程中调用）。
// totalBytes/processedBytes 为需要处理/已完成（均不含跳过文件）的字节数，
// 用于按实际工作量估算 ETA，避免跳过文件把速率拉高导致预计过于乐观。
type ProgressFunc func(phase string, total, processed, succeeded, skipped, failed int, totalBytes, processedBytes int64, rate float64, etaSeconds *int64)

// ScanResult 扫描执行结果。
type ScanResult struct {
	Session        *ScanSession
	Found          int
	Imported       int
	Skipped        int
	Failed         int
	Cleaned        int
	TotalBytes     int64 // 需要处理（不含跳过）的文件总字节数
	ProcessedBytes int64 // 已完成处理（含失败，不含跳过）的文件字节数
}

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

// ThumbRef 缩略图引用（删除媒体时收集，用于按类型解析根目录后清理物理文件）。
type ThumbRef struct {
	Kind string // image / video
	Rel  string // 相对该类型缩略图根目录的路径
}

// TaskKind 任务类型。
type TaskKind string

const (
	TaskKindScan            TaskKind = "scan"
	TaskKindRepair          TaskKind = "repair"
	TaskKindTemporaryScan   TaskKind = "temporary_scan"
	TaskKindReportImage     TaskKind = "report_image"
	TaskKindReportVideo     TaskKind = "report_video"
	TaskKindReportDuplicate TaskKind = "report_duplicate"
	TaskKindPromote         TaskKind = "promote"
	TaskKindDirectoryDelete TaskKind = "directory_delete"
	TaskKindScanSha1        TaskKind = "scan_sha1"
)

// TaskStatus 任务状态。
type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// BackgroundTask 后台任务。
type BackgroundTask struct {
	ID             string     `json:"id"`
	Kind           TaskKind   `json:"kind"`
	Status         TaskStatus `json:"status"`
	Title          string     `json:"title"`
	DedupeKey      *string    `json:"dedupe_key,omitempty"`
	LibraryID      *int64     `json:"library_id,omitempty"`
	ScanSessionID  *string    `json:"scan_session_id,omitempty"`
	PayloadJSON    *string    `json:"payload_json,omitempty"`
	Phase          string     `json:"phase"`
	TotalItems     int        `json:"total_items"`
	ProcessedItems int        `json:"processed_items"`
	SucceededItems int        `json:"succeeded_items"`
	SkippedItems   int        `json:"skipped_items"`
	FailedItems    int        `json:"failed_items"`
	ResultJSON     *string    `json:"result_json,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	ProcessingRate float64    `json:"processing_rate"`
	EtaSeconds     *int64     `json:"eta_seconds,omitempty"`
	QueuedAt       time.Time  `json:"queued_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	QueuePosition  int        `json:"queue_position,omitempty"` // 动态计算，不持久化
}

// FileStats 文件统计记录。
type FileStats struct {
	ID         int64     `json:"id"`
	DirPath    string    `json:"dir_path"`
	TotalBytes int64     `json:"total_bytes"`
	TotalCount int64     `json:"total_count"`
	ExtStats   string    `json:"ext_stats"` // JSON 字符串
	FileTree   string    `json:"file_tree"` // JSON 字符串
	CreatedAt  time.Time `json:"created_at"`
}
