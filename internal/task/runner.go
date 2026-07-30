// runner.go：任务调度器，单消费者串行执行所有后台任务。
// 代码注释使用中文。
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"memable/internal/duplicate"
	"memable/internal/repo"
)

// Runner 任务调度器，串行执行 queued 任务。
type Runner struct {
	Tasks       *repo.TaskRepo
	Sessions    *repo.SessionRepo
	Media       *repo.MediaRepo
	Libraries   *repo.LibraryRepo
	ScanSvc     ScanExecutor
	Config      RunnerConfig
	cancelFuncs sync.Map // taskID -> context.CancelFunc
	running     bool
	mu          sync.Mutex
	stopCh      chan struct{}
}

// RunnerConfig 调度器配置。
type RunnerConfig struct {
	PoolSize  int
	ThumbBase string
}

// ScanExecutor 扫描执行器接口。
type ScanExecutor interface {
	ExecuteScan(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error)
}

// NewRunner 创建任务调度器。
func NewRunner(tasks *repo.TaskRepo, sessions *repo.SessionRepo, media *repo.MediaRepo,
	libraries *repo.LibraryRepo, scanSvc ScanExecutor, cfg RunnerConfig) *Runner {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 4
	}
	return &Runner{
		Tasks:     tasks,
		Sessions:  sessions,
		Media:     media,
		Libraries: libraries,
		ScanSvc:   scanSvc,
		Config:    cfg,
		stopCh:    make(chan struct{}),
	}
}

// Start 启动调度器，重置遗留任务后开始消费队列。
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	// 重置遗留的 running 任务
	if n, err := r.Tasks.ResetRunning(); err != nil {
		slog.Error("重置遗留任务失败", "err", err)
	} else if n > 0 {
		slog.Info("重置遗留运行任务", "count", n)
	}

	go r.loop(ctx)
	slog.Info("任务调度器已启动")
}

// Stop 停止调度器。
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	close(r.stopCh)
	r.running = false
	slog.Info("任务调度器已停止")
}

// CancelTask 取消指定任务。
func (r *Runner) CancelTask(taskID string) error {
	task, err := r.Tasks.GetByID(taskID)
	if err != nil {
		return err
	}
	if task.Status == repo.TaskStatusQueued {
		return r.Tasks.Cancel(taskID)
	}
	if task.Status == repo.TaskStatusRunning {
		if v, ok := r.cancelFuncs.Load(taskID); ok {
			v.(context.CancelFunc)()
			r.cancelFuncs.Delete(taskID)
		}
		return r.Tasks.Cancel(taskID)
	}
	return fmt.Errorf("任务 %s 处于终态 %s，无法取消", taskID, task.Status)
}

// Enqueue 提交任务到队列。
func (r *Runner) Enqueue(kind repo.TaskKind, title string, dedupeKey *string, libraryID *int64, payload any) (*repo.BackgroundTask, error) {
	// 去重检查
	if dedupeKey != nil {
		existing, err := r.Tasks.FindActiveByDedupe(*dedupeKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, fmt.Errorf("duplicate")
		}
	}

	var payloadJSON *string
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化任务参数: %w", err)
		}
		s := string(b)
		payloadJSON = &s
	}

	task := &repo.BackgroundTask{
		ID:          uuid.NewString(),
		Kind:        kind,
		Status:      repo.TaskStatusQueued,
		Title:       title,
		DedupeKey:   dedupeKey,
		LibraryID:   libraryID,
		PayloadJSON: payloadJSON,
		Phase:       "queued",
	}
	if err := r.Tasks.Create(task); err != nil {
		return nil, err
	}

	// 尝试立即唤醒调度器（非阻塞）
	select {
	case r.stopCh <- struct{}{}:
	default:
	}
	return task, nil
}

// loop 主循环：串行消费队列。
func (r *Runner) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		default:
		}

		task, err := r.Tasks.DequeueNext()
		if err != nil {
			slog.Error("取出任务失败", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if task == nil {
			// 无任务，等待唤醒或定时轮询
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-ticker.C:
				continue
			}
		}

		r.executeTask(ctx, task)
	}
}

// executeTask 执行单个任务。
func (r *Runner) executeTask(ctx context.Context, task *repo.BackgroundTask) {
	taskCtx, cancel := context.WithCancel(ctx)
	r.cancelFuncs.Store(task.ID, cancel)
	defer r.cancelFuncs.Delete(task.ID)

	slog.Info("开始执行任务", "task_id", task.ID, "kind", task.Kind, "title", task.Title)
	progress := repo.ProgressFunc(func(phase string, total, processed, succeeded, skipped, failed int) {
		_ = r.Tasks.UpdateProgress(task.ID, phase, total, processed, succeeded, skipped, failed)
	})

	var execErr error
	switch task.Kind {
	case repo.TaskKindScan:
		execErr = r.execScan(taskCtx, task, false, progress)
	case repo.TaskKindRepair:
		// 兼容升级前已经持久化的修复任务，统一按强制同步扫描执行。
		execErr = r.execLegacyRepair(taskCtx, task, progress)
	case repo.TaskKindTemporaryScan:
		execErr = r.execScan(taskCtx, task, true, progress)
	case repo.TaskKindReportImage:
		execErr = r.execReport(taskCtx, task, "image", progress)
	case repo.TaskKindReportVideo:
		execErr = r.execReport(taskCtx, task, "video", progress)
	case repo.TaskKindPromote:
		execErr = r.execPromote(taskCtx, task, progress)
	case repo.TaskKindDirectoryDelete:
		execErr = r.execDirectoryDelete(taskCtx, task, progress)
	default:
		execErr = fmt.Errorf("未知任务类型: %s", task.Kind)
	}

	// 检查是否已被取消
	if taskCtx.Err() != nil {
		_ = r.Tasks.Cancel(task.ID)
		slog.Info("任务已取消", "task_id", task.ID)
		return
	}

	if execErr != nil {
		_ = r.Tasks.Fail(task.ID, execErr.Error())
		slog.Error("任务执行失败", "task_id", task.ID, "err", execErr)
	} else {
		_ = r.Tasks.Complete(task.ID, "{}")
		slog.Info("任务执行完成", "task_id", task.ID)
	}
}

// ScanPayload 扫描任务参数。
type ScanPayload struct {
	LibraryPath string `json:"library_path"`
	LibraryName string `json:"library_name"`
	LibraryKind string `json:"library_kind"`
	Force       bool   `json:"force"`
}

// execScan 执行扫描任务。
func (r *Runner) execScan(ctx context.Context, task *repo.BackgroundTask, temporary bool, progress repo.ProgressFunc) error {
	var payload ScanPayload
	if task.PayloadJSON != nil {
		if err := json.Unmarshal([]byte(*task.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("解析扫描参数: %w", err)
		}
	}

	// 创建或复用收藏库
	var lib repo.Library
	if task.LibraryID != nil {
		l, err := r.Libraries.GetByID(*task.LibraryID)
		if err != nil {
			return fmt.Errorf("查询收藏库: %w", err)
		}
		lib = *l
	} else {
		lib = repo.Library{
			Name: payload.LibraryName,
			Path: payload.LibraryPath,
			Kind: payload.LibraryKind,
		}
		if err := r.Libraries.Create(&lib); err != nil {
			return fmt.Errorf("创建收藏库: %w", err)
		}
		if task.LibraryID == nil {
			task.LibraryID = &lib.ID
		}
	}

	sessionID := uuid.NewString()
	_ = r.Tasks.UpdateProgress(task.ID, "discovering", 0, 0, 0, 0, 0)

	result, err := r.ScanSvc.ExecuteScan(ctx, lib, sessionID, temporary, payload.Force, r.Config.PoolSize, progress)
	if err != nil {
		return err
	}

	resJSON, _ := json.Marshal(map[string]any{
		"session_id": result.Session.ID,
		"library_id": lib.ID,
		"cleaned":    result.Cleaned,
	})
	_ = r.Tasks.UpdateProgress(task.ID, "done",
		result.Found, result.Imported+result.Skipped+result.Failed,
		result.Imported, result.Skipped, result.Failed)
	_ = r.Tasks.Complete(task.ID, string(resJSON))
	return nil
}

// execLegacyRepair 执行升级前遗留的修复任务。
func (r *Runner) execLegacyRepair(ctx context.Context, task *repo.BackgroundTask, progress repo.ProgressFunc) error {
	if task.LibraryID == nil {
		return fmt.Errorf("修复任务缺少 library_id")
	}
	lib, err := r.Libraries.GetByID(*task.LibraryID)
	if err != nil {
		return fmt.Errorf("查询收藏库: %w", err)
	}

	sessionID := uuid.NewString()
	_ = r.Tasks.UpdateProgress(task.ID, "discovering", 0, 0, 0, 0, 0)

	result, err := r.ScanSvc.ExecuteScan(ctx, *lib, sessionID, false, true, r.Config.PoolSize, progress)
	if err != nil {
		return err
	}

	resJSON, _ := json.Marshal(map[string]any{
		"session_id": result.Session.ID,
		"library_id": lib.ID,
	})
	_ = r.Tasks.UpdateProgress(task.ID, "done",
		result.Found, result.Imported+result.Skipped+result.Failed,
		result.Imported, result.Skipped, result.Failed)
	_ = r.Tasks.Complete(task.ID, string(resJSON))
	return nil
}

// ReportPayload 报告任务参数。
type ReportPayload struct {
	OutputPath string `json:"output_path"`
}

// execReport 执行报告生成任务。
func (r *Runner) execReport(ctx context.Context, task *repo.BackgroundTask, kind string, progress repo.ProgressFunc) error {
	var payload ReportPayload
	if task.PayloadJSON != nil {
		if err := json.Unmarshal([]byte(*task.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("解析报告参数: %w", err)
		}
	}
	if payload.OutputPath == "" {
		if kind == "image" {
			payload.OutputPath = "report_image.html"
		} else {
			payload.OutputPath = "report_video.html"
		}
	}

	det := duplicate.NewDetector(r.Media, nil)
	progress("detecting", 0, 0, 0, 0, 0)

	var groups []duplicate.Group
	var err error
	if kind == "image" {
		groups, err = det.DetectImageDuplicates()
	} else {
		groups, err = det.DetectVideoDuplicates()
	}
	if err != nil {
		return err
	}

	progress("writing_report", 0, 0, 0, 0, 0)
	libs, _ := r.Libraries.List()
	if err := duplicate.GenerateHTMLReport(groups, libs, kind, r.Config.ThumbBase, payload.OutputPath); err != nil {
		return err
	}

	result, _ := json.Marshal(map[string]any{
		"report_path": payload.OutputPath,
		"groups":      len(groups),
	})
	_ = r.Tasks.Complete(task.ID, string(result))
	return nil
}

// PromotePayload 入库任务参数。
type PromotePayload struct {
	SessionID string `json:"session_id"`
	LibraryID int64  `json:"library_id"`
}

// execPromote 执行临时扫描入库任务。
func (r *Runner) execPromote(ctx context.Context, task *repo.BackgroundTask, progress repo.ProgressFunc) error {
	var payload PromotePayload
	if task.PayloadJSON != nil {
		if err := json.Unmarshal([]byte(*task.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("解析入库参数: %w", err)
		}
	}

	session, err := r.Sessions.GetByID(payload.SessionID)
	if err != nil {
		return fmt.Errorf("查询扫描会话: %w", err)
	}
	if !session.IsTemporary {
		return fmt.Errorf("会话 %s 不是临时扫描", payload.SessionID)
	}

	targetLib, err := r.Libraries.GetByID(payload.LibraryID)
	if err != nil {
		return fmt.Errorf("查询目标收藏库: %w", err)
	}

	// 获取源库路径
	var srcBasePath string
	if session.LibraryID != nil {
		srcLib, err := r.Libraries.GetByID(*session.LibraryID)
		if err == nil {
			srcBasePath = srcLib.Path
		}
	}

	medias, err := r.Media.ListBySession(payload.SessionID)
	if err != nil {
		return err
	}

	total := len(medias)
	moved := 0
	for i, m := range medias {
		if srcBasePath != "" {
			if err := moveFile(
				srcBasePath+m.RelativePath,
				targetLib.Path+m.RelativePath,
			); err != nil {
				slog.Warn("移动文件失败", "src", m.RelativePath, "err", err)
			} else {
				moved++
			}
		}
		if err := r.Media.UpdateLibrary(m.ID, payload.LibraryID); err != nil {
			slog.Error("更新媒体库归属失败", "media_id", m.ID, "err", err)
		}
		if (i+1)%50 == 0 || i+1 == total {
			progress("promoting", total, i+1, moved, 0, 0)
		}
	}

	_ = r.Sessions.Promote(payload.SessionID)
	if session.LibraryID != nil {
		_ = r.Libraries.Delete(*session.LibraryID)
	}

	result, _ := json.Marshal(map[string]any{
		"moved":   moved,
		"library": targetLib.Name,
	})
	_ = r.Tasks.Complete(task.ID, string(result))
	return nil
}

// moveFile 移动文件（跨目录）。
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// DirectoryDeletePayload 目录删除任务参数。
type DirectoryDeletePayload struct {
	LibraryID int64  `json:"library_id"`
	DirPath   string `json:"dir_path"`
}

// execDirectoryDelete 执行目录删除任务：删除本地目录、数据库媒体记录和无引用缩略图。
func (r *Runner) execDirectoryDelete(ctx context.Context, task *repo.BackgroundTask, progress repo.ProgressFunc) error {
	var payload DirectoryDeletePayload
	if task.PayloadJSON != nil {
		if err := json.Unmarshal([]byte(*task.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("解析目录删除参数: %w", err)
		}
	}
	if payload.LibraryID == 0 || payload.DirPath == "" {
		return fmt.Errorf("目录删除参数不完整")
	}

	lib, err := r.Libraries.GetByID(payload.LibraryID)
	if err != nil {
		return fmt.Errorf("查询收藏库: %w", err)
	}

	// 阶段1：删除数据库媒体记录，收集缩略图路径
	progress("deleting_db", 0, 0, 0, 0, 0)
	thumbPaths, err := r.Media.DeleteByDirectory(payload.LibraryID, payload.DirPath)
	if err != nil {
		return fmt.Errorf("删除数据库记录: %w", err)
	}

	// 阶段2：删除本地目录
	progress("deleting_files", 0, 0, 0, 0, 0)
	absDir := filepath.Join(lib.Path, payload.DirPath)
	if err := os.RemoveAll(absDir); err != nil && !os.IsNotExist(err) {
		slog.Warn("删除本地目录失败（数据库已清理）", "path", absDir, "err", err)
	}

	// 阶段3：清理无引用缩略图
	progress("cleaning_thumbs", 0, len(thumbPaths), 0, 0, 0)
	deleted := 0
	for i, thumbRel := range thumbPaths {
		n, err := r.Media.CountThumbnailReferences(thumbRel)
		if err != nil || n > 0 {
			continue
		}
		thumbAbs := filepath.Join(r.Config.ThumbBase, thumbRel)
		if err := os.Remove(thumbAbs); err != nil && !os.IsNotExist(err) {
			slog.Warn("删除缩略图失败", "path", thumbAbs, "err", err)
			continue
		}
		// 清理空的哈希分片目录
		_ = os.Remove(filepath.Dir(thumbAbs))
		deleted++
		if (i+1)%50 == 0 || i+1 == len(thumbPaths) {
			progress("cleaning_thumbs", len(thumbPaths), i+1, deleted, 0, 0)
		}
	}

	result, _ := json.Marshal(map[string]any{
		"deleted_thumbs": deleted,
		"dir_path":       payload.DirPath,
	})
	_ = r.Tasks.Complete(task.ID, string(result))
	return nil
}
