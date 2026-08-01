// runner.go：任务调度器。主队列串行执行扫描等普通任务，
// 报告队列单独串行执行生成报告类任务，两条队列互不阻塞。
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
	"memable/internal/media"
	"memable/internal/recycle"
	"memable/internal/repo"
	"memable/internal/worker"
)

// Runner 任务调度器。维护两条独立队列：
// 主队列（扫描等）与报告队列（生成报告类任务），各自串行消费、互不阻塞。
type Runner struct {
	Tasks       *repo.TaskRepo
	Sessions    *repo.SessionRepo
	Media       *repo.MediaRepo
	Libraries   *repo.LibraryRepo
	ScanSvc     ScanExecutor
	Dup         *duplicate.Service
	Config      RunnerConfig
	cancelFuncs sync.Map // taskID -> context.CancelFunc
	running     bool
	mu          sync.Mutex
	wakeCh      chan struct{} // 主队列唤醒信号
	reportWake  chan struct{} // 报告队列唤醒信号
	stopCh      chan struct{}
}

// RunnerConfig 调度器配置。
type RunnerConfig struct {
	PoolSize        int
	ImageThumbBase  string
	VideoThumbBase  string
	PermanentDelete bool // true=永久删除源文件；false=移入系统回收站
}

// ScanExecutor 扫描执行器接口。
type ScanExecutor interface {
	ExecuteScan(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error)
}

// NewRunner 创建任务调度器。
func NewRunner(tasks *repo.TaskRepo, sessions *repo.SessionRepo, media *repo.MediaRepo,
	libraries *repo.LibraryRepo, scanSvc ScanExecutor, cfg RunnerConfig, dup *duplicate.Service) *Runner {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 4
	}
	return &Runner{
		Tasks:      tasks,
		Sessions:   sessions,
		Media:      media,
		Libraries:  libraries,
		ScanSvc:    scanSvc,
		Dup:        dup,
		Config:     cfg,
		wakeCh:     make(chan struct{}, 1),
		reportWake: make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
	}
}

// Start 启动调度器，重置遗留任务后开始消费两条队列。
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
	go r.reportLoop(ctx)
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

	// 尝试立即唤醒所属队列的调度器（非阻塞）。唤醒和停止必须使用不同通道。
	wake := r.wakeCh
	if repo.IsReportKind(kind) {
		wake = r.reportWake
	}
	select {
	case wake <- struct{}{}:
	default:
	}
	return task, nil
}

// loop 主循环：串行消费主队列（报告类任务除外）。
func (r *Runner) loop(ctx context.Context) {
	r.consume(ctx, r.wakeCh, r.Tasks.DequeueNext)
}

// reportLoop 报告队列循环：串行消费报告类任务，与其他任务互不影响。
func (r *Runner) reportLoop(ctx context.Context) {
	r.consume(ctx, r.reportWake, r.Tasks.DequeueNextReport)
}

// consume 消费指定队列：循环取出最早的 queued 任务并执行，队列为空时等待唤醒或定时轮询。
func (r *Runner) consume(ctx context.Context, wakeCh chan struct{}, dequeue func() (*repo.BackgroundTask, error)) {
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

		task, err := dequeue()
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
			case <-wakeCh:
				continue
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

	// 双口径吞吐估算 + 节流写库（最多 500ms 一次）。
	// 字节口径与文件口径均按“实际工作”计算（跳过文件不计入速率，避免虚高）：
	//   - 字节速率：已完成工作字节 / 时间，覆盖“剩余都是大文件”的场景；
	//   - 文件速率：已完成工作文件数 / 时间，覆盖“剩余都是小文件/字节统计未全量”的场景；
	// ETA 取两条口径的较大值（保守），并对 ETA 做轻量平滑避免跳变。
	var lastProcessedBytes int64
	var lastProcessedWorkFiles int
	var lastSampleTime time.Time
	var byteRate, fileRate float64
	var lastEta float64
	var lastFlush time.Time

	progress := repo.ProgressFunc(func(phase string, total, processed, succeeded, skipped, failed int, totalBytes, processedBytes int64, rate float64, eta *int64) {
		now := time.Now()
		workFiles := processed - skipped
		if !lastSampleTime.IsZero() {
			elapsed := now.Sub(lastSampleTime).Seconds()
			if elapsed >= 0.5 {
				byteInstant := float64(processedBytes-lastProcessedBytes) / elapsed
				fileInstant := float64(workFiles-lastProcessedWorkFiles) / elapsed
				if byteInstant > 0 {
					if byteRate == 0 {
						byteRate = byteInstant
					} else {
						byteRate = 0.25*byteInstant + 0.75*byteRate
					}
				}
				if fileInstant > 0 {
					if fileRate == 0 {
						fileRate = fileInstant
					} else {
						fileRate = 0.25*fileInstant + 0.75*fileRate
					}
				}
			}
		}
		lastProcessedBytes = processedBytes
		lastProcessedWorkFiles = workFiles
		lastSampleTime = now

		// 节流：最少 500ms 写一次库
		if !lastFlush.IsZero() && now.Sub(lastFlush) < 500*time.Millisecond {
			return
		}
		lastFlush = now

		// ETA：双口径取较大值（保守）。跳过文件不参与速率；
		// 剩余文件数按已见跳过比例折算未来跳过文件。
		var etaPtr *int64
		var etaSeconds float64 = -1
		remainingBytes := totalBytes - processedBytes
		if byteRate > 0 && remainingBytes > 0 {
			etaSeconds = float64(remainingBytes) / byteRate
		}
		if fileRate > 0 && processed > skipped {
			skipRatio := float64(skipped) / float64(processed)
			remainingWorkFiles := int64(float64(total-processed) * (1 - skipRatio))
			if remainingWorkFiles > 0 {
				e := float64(remainingWorkFiles) / fileRate
				if e > etaSeconds {
					etaSeconds = e
				}
			}
		}
		if etaSeconds >= 0 {
			// 轻量平滑：新值占 30%，避免 ETA 频繁跳变
			if lastEta > 0 {
				etaSeconds = 0.3*etaSeconds + 0.7*lastEta
			}
			lastEta = etaSeconds
			e := int64(etaSeconds)
			etaPtr = &e
		}
		_ = r.Tasks.UpdateProgress(task.ID, repo.TaskProgress{
			Phase: phase, Total: total, Processed: processed,
			Succeeded: succeeded, Skipped: skipped, Failed: failed,
			ProcessingRate: byteRate, EtaSeconds: etaPtr,
		})
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
	case repo.TaskKindReportDuplicate:
		execErr = r.execDuplicateReport(taskCtx, task, progress)
	case repo.TaskKindPromote:
		execErr = r.execPromote(taskCtx, task, progress)
	case repo.TaskKindDirectoryDelete:
		execErr = r.execDirectoryDelete(taskCtx, task, progress)
	case repo.TaskKindScanSha1:
		execErr = r.execScanSha1(taskCtx, task, progress)
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
		// 各执行器可能已写入结构化结果，未完成时才写入空结果。
		stored, err := r.Tasks.GetByID(task.ID)
		if err != nil || stored.Status != repo.TaskStatusCompleted {
			_ = r.Tasks.Complete(task.ID, "{}")
		}
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
	_ = r.Tasks.UpdateProgress(task.ID, repo.TaskProgress{Phase: "discovering", Total: 0, Processed: 0, Succeeded: 0, Skipped: 0, Failed: 0})

	result, err := r.ScanSvc.ExecuteScan(ctx, lib, sessionID, temporary, payload.Force, r.Config.PoolSize, progress)
	if err != nil {
		return err
	}

	// 媒体变化后维护重复报告：新增/重新处理标记 stale，删除类变更清理分组并刷新统计
	if r.Dup != nil {
		if result.Imported > 0 || result.Cleaned > 0 || payload.Force {
			_ = r.Dup.SetStale()
		}
		_ = r.Dup.PruneAfterMediaChange()
	}

	resJSON, _ := json.Marshal(map[string]any{
		"session_id":      result.Session.ID,
		"library_id":      lib.ID,
		"cleaned":         result.Cleaned,
		"total_bytes":     result.TotalBytes,
		"processed_bytes": result.ProcessedBytes,
	})
	_ = r.Tasks.UpdateProgress(task.ID, repo.TaskProgress{
		Phase: "done", Total: result.Found,
		Processed: result.Imported + result.Skipped + result.Failed,
		Succeeded: result.Imported, Skipped: result.Skipped, Failed: result.Failed,
	})
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
	_ = r.Tasks.UpdateProgress(task.ID, repo.TaskProgress{Phase: "discovering", Total: 0, Processed: 0, Succeeded: 0, Skipped: 0, Failed: 0})

	result, err := r.ScanSvc.ExecuteScan(ctx, *lib, sessionID, false, true, r.Config.PoolSize, progress)
	if err != nil {
		return err
	}

	resJSON, _ := json.Marshal(map[string]any{
		"session_id":      result.Session.ID,
		"library_id":      lib.ID,
		"total_bytes":     result.TotalBytes,
		"processed_bytes": result.ProcessedBytes,
	})
	_ = r.Tasks.UpdateProgress(task.ID, repo.TaskProgress{
		Phase: "done", Total: result.Found,
		Processed: result.Imported + result.Skipped + result.Failed,
		Succeeded: result.Imported, Skipped: result.Skipped, Failed: result.Failed,
	})
	_ = r.Tasks.Complete(task.ID, string(resJSON))
	return nil
}

// execScanSha1 执行补齐 SHA1 任务：为该收藏库中 sha1 缺失的记录计算并写入 SHA1。
// 只处理缺失记录、不强制重算；用 worker pool 并发读取文件，支持取消与进度统计。
func (r *Runner) execScanSha1(ctx context.Context, task *repo.BackgroundTask, progress repo.ProgressFunc) error {
	if task.LibraryID == nil {
		return fmt.Errorf("补齐 SHA1 任务缺少 library_id")
	}
	lib, err := r.Libraries.GetByID(*task.LibraryID)
	if err != nil {
		return fmt.Errorf("查询收藏库: %w", err)
	}

	missing, err := r.Media.ListMissingSha1(lib.ID)
	if err != nil {
		return err
	}
	total := len(missing)

	// 计数与进度上报：worker 并发更新计数，progress 调用加锁串行化
	// （runner 的 EWMA 闭包非并发安全，需保证同一时刻只有一个调用者）。
	var mu sync.Mutex
	var processed, succeeded, failed int
	report := func() {
		mu.Lock()
		p, s, f := processed, succeeded, failed
		mu.Unlock()
		progress("hashing", total, p, s, 0, f, 0, 0, 0, (*int64)(nil))
	}

	pool := worker.NewPool(r.Config.PoolSize)
	pool.Start(ctx)
	for _, item := range missing {
		item := item
		if !pool.Submit(&worker.ScanJob{
			Run: func(jobCtx context.Context) error {
				slog.Info("处理文件", "dir", filepath.Dir(item.RelativePath), "file", filepath.Base(item.RelativePath))
				mu.Lock()
				processed++
				mu.Unlock()
				abs := filepath.Join(lib.Path, filepath.FromSlash(item.RelativePath))
				sha, err := media.SHA1File(abs)
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					report()
					return fmt.Errorf("计算 SHA1 %q: %w", abs, err)
				}
				if err := r.Media.UpdateSha1(item.ID, sha); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					report()
					return err
				}
				mu.Lock()
				succeeded++
				mu.Unlock()
				report()
				return nil
			},
		}) {
			// 任务已取消/池已关闭：未派发的记录按失败计数
			mu.Lock()
			processed++
			failed++
			mu.Unlock()
		}
	}
	pool.Stop()

	mu.Lock()
	p, s, f := processed, succeeded, failed
	mu.Unlock()
	resJSON, _ := json.Marshal(map[string]any{
		"library_id": lib.ID,
		"checked":    total,
		"updated":    s,
		"failed":     f,
	})
	_ = r.Tasks.UpdateProgress(task.ID, repo.TaskProgress{
		Phase: "done", Total: total,
		Processed: p, Succeeded: s, Skipped: 0, Failed: f,
	})
	_ = r.Tasks.Complete(task.ID, string(resJSON))

	// sha1 补齐后重复报告可能产生新的 sha1_exact 分组，标记旧报告失效
	if s > 0 && r.Dup != nil {
		_ = r.Dup.SetStale()
	}
	return nil
}

// execReport 执行重复检测并将结构化结果保存到任务中。
func (r *Runner) execReport(ctx context.Context, task *repo.BackgroundTask, kind string, progress repo.ProgressFunc) error {
	det := duplicate.NewDetector(r.Media, nil)
	progress("detecting", 0, 0, 0, 0, 0, 0, 0, 0, (*int64)(nil))

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

	libs, err := r.Libraries.List()
	if err != nil {
		return fmt.Errorf("查询收藏库: %w", err)
	}
	result, err := json.Marshal(duplicate.BuildReport(groups, libs, kind))
	if err != nil {
		return fmt.Errorf("序列化重复检测结果: %w", err)
	}
	progress("done", len(groups), len(groups), len(groups), 0, 0, 0, 0, 0, (*int64)(nil))
	_ = r.Tasks.Complete(task.ID, string(result))
	return nil
}

// ReportPayload 重复报告任务参数。
type ReportPayload struct {
	Options duplicate.Options `json:"options"`
}

// execDuplicateReport 执行重复报告生成并持久化到三张表。
func (r *Runner) execDuplicateReport(ctx context.Context, task *repo.BackgroundTask, progress repo.ProgressFunc) error {
	var payload ReportPayload
	if task.PayloadJSON != nil {
		if err := json.Unmarshal([]byte(*task.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("解析重复报告参数: %w", err)
		}
	}
	if r.Dup == nil {
		return fmt.Errorf("重复报告服务未初始化")
	}
	progress("detecting", 0, 0, 0, 0, 0, 0, 0, 0, (*int64)(nil))
	rep, err := r.Dup.Generate(payload.Options, task.ID)
	if err != nil {
		return err
	}
	result, _ := json.Marshal(map[string]any{
		"report_id":    rep.ID,
		"total_groups": rep.TotalGroups,
		"total_files":  rep.TotalFiles,
		"media_type":   rep.MediaType,
		"scope":        rep.Scope,
	})
	progress("done", rep.TotalGroups, rep.TotalGroups, rep.TotalGroups, 0, 0, 0, 0, 0, (*int64)(nil))
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
			progress("promoting", total, i+1, moved, 0, 0, 0, 0, 0, (*int64)(nil))
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
	progress("deleting_db", 0, 0, 0, 0, 0, 0, 0, 0, (*int64)(nil))
	_, thumbRefs, err := r.Media.DeleteByDirectory(payload.LibraryID, payload.DirPath)
	if err != nil {
		return fmt.Errorf("删除数据库记录: %w", err)
	}

	// 阶段2：删除本地目录（按配置：永久删除或移入回收站）
	progress("deleting_files", 0, 0, 0, 0, 0, 0, 0, 0, (*int64)(nil))
	absDir := filepath.Join(lib.Path, payload.DirPath)
	var dirErr error
	if r.Config.PermanentDelete {
		dirErr = os.RemoveAll(absDir)
	} else {
		dirErr = recycle.ToBinDir(absDir)
	}
	if dirErr != nil && !os.IsNotExist(dirErr) {
		slog.Warn("删除本地目录失败（数据库已清理）", "path", absDir, "err", dirErr)
	}

	// 阶段3：清理无引用缩略图
	progress("cleaning_thumbs", 0, len(thumbRefs), 0, 0, 0, 0, 0, 0, (*int64)(nil))
	deleted := 0
	for i, ref := range thumbRefs {
		n, err := r.Media.CountThumbnailReferences(ref.Rel)
		if err != nil || n > 0 {
			continue
		}
		thumbAbs := filepath.Join(r.thumbBaseFor(ref.Kind), filepath.FromSlash(ref.Rel))
		if err := os.Remove(thumbAbs); err != nil && !os.IsNotExist(err) {
			slog.Warn("删除缩略图失败", "path", thumbAbs, "err", err)
			continue
		}
		// 清理空的哈希分片目录
		_ = os.Remove(filepath.Dir(thumbAbs))
		deleted++
		if (i+1)%50 == 0 || i+1 == len(thumbRefs) {
			progress("cleaning_thumbs", len(thumbRefs), i+1, deleted, 0, 0, 0, 0, 0, (*int64)(nil))
		}
	}

	// 媒体删除后维护重复报告（级联成员 → 清理不足 2 个的组 → 更新统计）
	if r.Dup != nil {
		_ = r.Dup.PruneAfterMediaChange()
	}

	result, _ := json.Marshal(map[string]any{
		"deleted_thumbs": deleted,
		"dir_path":       payload.DirPath,
	})
	_ = r.Tasks.Complete(task.ID, string(result))
	return nil
}

// thumbBaseFor 按媒体类型返回缩略图根目录（配置优先，否则系统推荐目录）。
func (r *Runner) thumbBaseFor(kind string) string {
	if kind == "video" {
		if r.Config.VideoThumbBase != "" {
			return r.Config.VideoThumbBase
		}
		return filepath.Join(mustUserCacheDir(), "memable", "thumbnails", "video")
	}
	if r.Config.ImageThumbBase != "" {
		return r.Config.ImageThumbBase
	}
	return filepath.Join(mustUserCacheDir(), "memable", "thumbnails", "image")
}

// mustUserCacheDir 返回系统缓存目录（失败时回退临时目录）。
func mustUserCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return os.TempDir()
	}
	return dir
}
