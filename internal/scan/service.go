// 包 scan：扫描编排，将媒体采集结果写入数据库。
// 代码注释使用中文。
package scan

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"

	"memable/internal/config"
	"memable/internal/media"
	"memable/internal/repo"
	"memable/internal/worker"
)

// Service 扫描服务，负责遍历目录、增量判定、采集 metadata 并入库。
type Service struct {
	Sessions    *repo.SessionRepo
	Media       *repo.MediaRepo
	Config      *config.Config
	ThumbBase   string // 缩略图根目录（绝对路径）
	Libraries   *repo.LibraryRepo
	cancelFuncs sync.Map // sessionID -> context.CancelFunc
}

// Progress 扫描进度，供外部查询。
type Progress struct {
	SessionID string
	Status    string
	Found     int
	Skipped   int
	Imported  int
	Failed    int
}

// scanState 一次扫描的运行时状态（支持并发安全）。
type scanState struct {
	mu       sync.Mutex
	imported int
	skipped  int
	failed   int
}

// ScanLibraryAsync 使用 Worker Pool 并发扫描；返回 sessionID 和 error。
// 调用方可通过 Progress(sessionID) 查询进度。
// 注意：扫描在独立的 goroutine 中执行，不受 HTTP 请求生命周期影响。
func (s *Service) ScanLibraryAsync(ctx context.Context, lib repo.Library, sessionID string, temporary bool, poolSize int) (string, error) {
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	if err := s.Sessions.Create(&repo.ScanSession{
		ID:          sessionID,
		LibraryID:   &lib.ID,
		IsTemporary: temporary,
		Status:      "running",
	}); err != nil {
		return "", err
	}

	scanCtx, cancel := context.WithCancel(context.Background())
	s.cancelFuncs.Store(sessionID, cancel)
	go s.runScan(scanCtx, lib, sessionID, temporary, poolSize)
	return sessionID, nil
}

// runScan 实际执行扫描（在 goroutine 中运行）。
func (s *Service) runScan(ctx context.Context, lib repo.Library, sessionID string, temporary bool, poolSize int) {
	st := &scanState{}

	entries, err := media.Walk(ctx, lib.Path)
	if err != nil {
		slog.Error("遍历目录失败", "path", lib.Path, "err", err)
		_ = s.Sessions.UpdateStatus(sessionID, "failed")
		return
	}

	pool := worker.NewPool(poolSize)
	pool.Start(ctx)

	for _, e := range entries {
		need, err := s.Media.NeedScan(lib.ID, e.RelativePath, e.Mtime, e.Size)
		if err != nil {
			slog.Error("增量判定失败", "path", e.RelativePath, "err", err)
			st.mu.Lock()
			st.failed++
			st.mu.Unlock()
			continue
		}
		if !need {
			st.mu.Lock()
			st.skipped++
			st.mu.Unlock()
			continue
		}

		entry := e // 捕获循环变量
		pool.Submit(&worker.ScanJob{
			Run: func(jobCtx context.Context) error {
				m, err := s.collect(jobCtx, lib.ID, sessionID, entry)
				if err != nil {
					slog.Warn("采集失败", "path", entry.AbsPath, "err", err)
					st.mu.Lock()
					st.failed++
					st.mu.Unlock()
					return err
				}
				if err := s.Media.Upsert(m); err != nil {
					slog.Error("入库失败", "path", entry.RelativePath, "err", err)
					st.mu.Lock()
					st.failed++
					st.mu.Unlock()
					return err
				}
				st.mu.Lock()
				st.imported++
				st.mu.Unlock()
				return nil
			},
		})
	}

	pool.Stop()

	if err := s.Sessions.UpdateStatus(sessionID, "completed"); err != nil {
		slog.Error("更新会话状态失败", "session", sessionID, "err", err)
	}
	slog.Info("异步扫描完成",
		"library_id", lib.ID, "session_id", sessionID,
		"found", len(entries), "imported", st.imported,
		"skipped", st.skipped, "failed", st.failed)
}

// RepairLibraryAsync 重复扫描：补采缺失元数据、补生成缩略图、采集新文件。
// 与 ScanLibraryAsync 的区别：不依赖 mtime/size 变化，而是检查记录完整性。
func (s *Service) RepairLibraryAsync(ctx context.Context, lib repo.Library, sessionID string, temporary bool, poolSize int) (string, error) {
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	if err := s.Sessions.Create(&repo.ScanSession{
		ID:          sessionID,
		LibraryID:   &lib.ID,
		IsTemporary: temporary,
		Status:      "running",
	}); err != nil {
		return "", err
	}

	scanCtx, cancel := context.WithCancel(context.Background())
	s.cancelFuncs.Store(sessionID, cancel)
	go s.runRepair(scanCtx, lib, sessionID, temporary, poolSize)
	return sessionID, nil
}

// runRepair 执行重复扫描（在 goroutine 中运行）。
func (s *Service) runRepair(ctx context.Context, lib repo.Library, sessionID string, temporary bool, poolSize int) {
	st := &scanState{}

	entries, err := media.Walk(ctx, lib.Path)
	if err != nil {
		slog.Error("遍历目录失败", "path", lib.Path, "err", err)
		_ = s.Sessions.UpdateStatus(sessionID, "failed")
		return
	}

	pool := worker.NewPool(poolSize)
	pool.Start(ctx)

	for _, e := range entries {
		need, err := s.Media.NeedRepair(lib.ID, e.RelativePath, string(e.Kind))
		if err != nil {
			slog.Error("完整性判定失败", "path", e.RelativePath, "err", err)
			st.mu.Lock()
			st.failed++
			st.mu.Unlock()
			continue
		}
		if !need {
			st.mu.Lock()
			st.skipped++
			st.mu.Unlock()
			continue
		}

		entry := e
		pool.Submit(&worker.ScanJob{
			Run: func(jobCtx context.Context) error {
				m, err := s.collect(jobCtx, lib.ID, sessionID, entry)
				if err != nil {
					slog.Warn("修补采集失败", "path", entry.AbsPath, "err", err)
					st.mu.Lock()
					st.failed++
					st.mu.Unlock()
					return err
				}
				if err := s.Media.Upsert(m); err != nil {
					slog.Error("修补入库失败", "path", entry.RelativePath, "err", err)
					st.mu.Lock()
					st.failed++
					st.mu.Unlock()
					return err
				}
				st.mu.Lock()
				st.imported++
				st.mu.Unlock()
				return nil
			},
		})
	}

	pool.Stop()

	if err := s.Sessions.UpdateStatus(sessionID, "completed"); err != nil {
		slog.Error("更新会话状态失败", "session", sessionID, "err", err)
	}
	slog.Info("重复扫描完成",
		"library_id", lib.ID, "session_id", sessionID,
		"found", len(entries), "repaired", st.imported,
		"skipped", st.skipped, "failed", st.failed)
}

// Stats 扫描统计。
type Stats struct {
	SessionID string
	Found     int
	Skipped   int
	Imported  int
	Failed    int
}

// ScanLibrary 扫描一个收藏库；sessionID 为空时自动生成 UUID。
func (s *Service) ScanLibrary(ctx context.Context, lib repo.Library, sessionID string, temporary bool) (*Stats, error) {
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	if err := s.Sessions.Create(&repo.ScanSession{
		ID:          sessionID,
		LibraryID:   &lib.ID,
		IsTemporary: temporary,
		Status:      "running",
	}); err != nil {
		return nil, err
	}

	stats := &Stats{SessionID: sessionID}
	entries, err := media.Walk(ctx, lib.Path)
	if err != nil {
		_ = s.Sessions.UpdateStatus(sessionID, "failed")
		return nil, err
	}
	stats.Found = len(entries)

	for _, e := range entries {
		need, err := s.Media.NeedScan(lib.ID, e.RelativePath, e.Mtime, e.Size)
		if err != nil {
			slog.Error("增量判定失败", "path", e.RelativePath, "err", err)
			stats.Failed++
			continue
		}
		if !need {
			stats.Skipped++
			continue
		}

		m, err := s.collect(ctx, lib.ID, sessionID, e)
		if err != nil {
			slog.Warn("采集失败", "path", e.AbsPath, "err", err)
			stats.Failed++
			continue
		}
		if err := s.Media.Upsert(m); err != nil {
			slog.Error("入库失败", "path", e.RelativePath, "err", err)
			stats.Failed++
			continue
		}
		stats.Imported++
	}

	if err := s.Sessions.UpdateStatus(sessionID, "completed"); err != nil {
		return nil, err
	}
	slog.Info("扫描完成",
		"library_id", lib.ID, "session_id", sessionID,
		"found", stats.Found, "imported", stats.Imported,
		"skipped", stats.Skipped, "failed", stats.Failed)
	return stats, nil
}

func (s *Service) collect(ctx context.Context, libraryID int64, sessionID string, e media.FileEntry) (*repo.Media, error) {
	sha1, err := media.SHA1File(e.AbsPath)
	if err != nil {
		return nil, fmt.Errorf("计算 SHA1 %q: %w", e.AbsPath, err)
	}
	m := &repo.Media{
		LibraryID:     libraryID,
		ScanSessionID: &sessionID,
		Kind:          string(e.Kind),
		RelativePath:  e.RelativePath,
		FileSize:      e.Size,
		Mtime:         e.Mtime,
		Sha1:          &sha1,
	}

	maxEdge := 300
	if s.Config != nil && s.Config.Thumbnail.MaxEdge > 0 {
		maxEdge = s.Config.Thumbnail.MaxEdge
	}

	thumbBase := s.ThumbBase
	if thumbBase == "" {
		thumbBase = "thumbnail"
	}

	switch e.Kind {
	case media.KindImage:
		meta, err := media.ProbeImage(e.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("读取图片 metadata %q: %w", e.AbsPath, err)
		}
		m.Format = &meta.Format
		m.Width = &meta.Width
		m.Height = &meta.Height

		hashes, err := media.ImagePerceptualHashes(e.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("计算图片相似哈希 %q: %w", e.AbsPath, err)
		}
		m.Phash = &hashes.PHash
		m.Dhash = &hashes.DHash
		m.Ahash = &hashes.AHash

		// 内容寻址缩略图
		storageKey := media.ThumbnailKey("image", sha1, maxEdge)
		thumbRel := media.ThumbnailStoragePath("image", storageKey)
		thumbAbs := filepath.Join(thumbBase, thumbRel)
		if err := ensureThumbnail(thumbAbs, func(tmp string) error {
			return media.GenerateImageThumbnail(e.AbsPath, tmp, maxEdge)
		}); err != nil {
			slog.Warn("生成图片缩略图失败", "path", e.AbsPath, "err", err)
		} else {
			m.ThumbnailPath = &thumbRel
		}

	case media.KindVideo:
		meta, err := media.ProbeVideo(ctx, e.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("读取视频 metadata %q: %w", e.AbsPath, err)
		}
		oshash, err := media.OSHashFile(e.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("计算视频 oshash %q: %w", e.AbsPath, err)
		}
		m.Format = &meta.Format
		m.Width = &meta.Width
		m.Height = &meta.Height
		m.DurationMs = &meta.DurationMs
		m.VideoCodec = &meta.VideoCodec
		m.AudioCodec = &meta.AudioCodec
		m.FrameRate = &meta.FrameRate
		m.BitRate = &meta.BitRate
		m.Oshash = &oshash

		// 内容寻址视频封面缩略图
		storageKey := media.ThumbnailKey("video", sha1, maxEdge)
		thumbRel := media.ThumbnailStoragePath("video", storageKey)
		thumbAbs := filepath.Join(thumbBase, thumbRel)
		if err := ensureThumbnail(thumbAbs, func(tmp string) error {
			_, err := media.ExtractVideoCover(ctx, e.AbsPath, tmp, maxEdge, meta.DurationMs)
			return err
		}); err != nil {
			slog.Warn("生成视频封面失败", "path", e.AbsPath, "err", err)
		} else {
			m.ThumbnailPath = &thumbRel
		}

		// 计算 sprite pHash（临时截图计算后自动清理）
		spritePhash, err := media.ComputeVideoSpritePHash(ctx, e.AbsPath, meta.DurationMs)
		if err != nil {
			slog.Warn("计算视频 sprite pHash 失败", "path", e.AbsPath, "err", err)
		} else {
			m.Phash = &spritePhash
		}
	}
	return m, nil
}

// ensureThumbnail 原子性地生成缩略图：目标文件已存在则跳过，否则生成到临时文件再重命名。
func ensureThumbnail(dst string, generate func(tmp string) error) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建缩略图目录: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".thumb-tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时缩略图文件: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	if err := generate(tmpPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		// 若目标已存在（另一 worker 抢先完成），则视为成功
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		return fmt.Errorf("安装缩略图: %w", err)
	}
	return nil
}

// CancelScan 取消扫描会话。
func (s *Service) CancelScan(sessionID string) error {
	if v, ok := s.cancelFuncs.Load(sessionID); ok {
		v.(context.CancelFunc)()
		s.cancelFuncs.Delete(sessionID)
	}
	return s.Sessions.UpdateStatus(sessionID, "cancelled")
}

// ExecuteScan 同步执行扫描，支持进度回调（供任务调度器调用）。
func (s *Service) ExecuteScan(ctx context.Context, lib repo.Library, sessionID string, temporary bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error) {
	if err := s.Sessions.Create(&repo.ScanSession{
		ID:          sessionID,
		LibraryID:   &lib.ID,
		IsTemporary: temporary,
		Status:      "running",
	}); err != nil {
		return nil, err
	}

	st := &scanState{}
	entries, err := media.Walk(ctx, lib.Path)
	if err != nil {
		_ = s.Sessions.UpdateStatus(sessionID, "failed")
		return nil, fmt.Errorf("遍历目录: %w", err)
	}
	progress("discovering", len(entries), 0, 0, 0, 0)

	pool := worker.NewPool(poolSize)
	pool.Start(ctx)

	for _, e := range entries {
		need, err := s.Media.NeedScan(lib.ID, e.RelativePath, e.Mtime, e.Size)
		if err != nil {
			slog.Error("增量判定失败", "path", e.RelativePath, "err", err)
			st.mu.Lock()
			st.failed++
			total := st.imported + st.skipped + st.failed
			st.mu.Unlock()
			progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
			continue
		}
		if !need {
			st.mu.Lock()
			st.skipped++
			total := st.imported + st.skipped + st.failed
			st.mu.Unlock()
			progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
			continue
		}

		entry := e
		pool.Submit(&worker.ScanJob{
			Run: func(jobCtx context.Context) error {
				m, err := s.collect(jobCtx, lib.ID, sessionID, entry)
				if err != nil {
					slog.Warn("采集失败", "path", entry.AbsPath, "err", err)
					st.mu.Lock()
					st.failed++
					total := st.imported + st.skipped + st.failed
					st.mu.Unlock()
					progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
					return err
				}
				if err := s.Media.Upsert(m); err != nil {
					slog.Error("入库失败", "path", entry.RelativePath, "err", err)
					st.mu.Lock()
					st.failed++
					total := st.imported + st.skipped + st.failed
					st.mu.Unlock()
					progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
					return err
				}
				st.mu.Lock()
				st.imported++
				total := st.imported + st.skipped + st.failed
				st.mu.Unlock()
				progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
				return nil
			},
		})
	}

	pool.Stop()
	_ = s.Sessions.UpdateStatus(sessionID, "completed")
	slog.Info("扫描完成",
		"library_id", lib.ID, "session_id", sessionID,
		"found", len(entries), "imported", st.imported,
		"skipped", st.skipped, "failed", st.failed)

	session, _ := s.Sessions.GetByID(sessionID)
	return &repo.ScanResult{
		Session:  session,
		Found:    len(entries),
		Imported: st.imported,
		Skipped:  st.skipped,
		Failed:   st.failed,
	}, nil
}

// ExecuteRepair 同步执行修复扫描，支持进度回调（供任务调度器调用）。
func (s *Service) ExecuteRepair(ctx context.Context, lib repo.Library, sessionID string, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error) {
	if err := s.Sessions.Create(&repo.ScanSession{
		ID:        sessionID,
		LibraryID: &lib.ID,
		Status:    "running",
	}); err != nil {
		return nil, err
	}

	st := &scanState{}
	entries, err := media.Walk(ctx, lib.Path)
	if err != nil {
		_ = s.Sessions.UpdateStatus(sessionID, "failed")
		return nil, fmt.Errorf("遍历目录: %w", err)
	}
	progress("discovering", len(entries), 0, 0, 0, 0)

	pool := worker.NewPool(poolSize)
	pool.Start(ctx)

	for _, e := range entries {
		need, err := s.Media.NeedRepair(lib.ID, e.RelativePath, string(e.Kind))
		if err != nil {
			slog.Error("完整性判定失败", "path", e.RelativePath, "err", err)
			st.mu.Lock()
			st.failed++
			total := st.imported + st.skipped + st.failed
			st.mu.Unlock()
			progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
			continue
		}
		if !need {
			st.mu.Lock()
			st.skipped++
			total := st.imported + st.skipped + st.failed
			st.mu.Unlock()
			progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
			continue
		}

		entry := e
		pool.Submit(&worker.ScanJob{
			Run: func(jobCtx context.Context) error {
				m, err := s.collect(jobCtx, lib.ID, sessionID, entry)
				if err != nil {
					slog.Warn("修补采集失败", "path", entry.AbsPath, "err", err)
					st.mu.Lock()
					st.failed++
					total := st.imported + st.skipped + st.failed
					st.mu.Unlock()
					progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
					return err
				}
				if err := s.Media.Upsert(m); err != nil {
					slog.Error("修补入库失败", "path", entry.RelativePath, "err", err)
					st.mu.Lock()
					st.failed++
					total := st.imported + st.skipped + st.failed
					st.mu.Unlock()
					progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
					return err
				}
				st.mu.Lock()
				st.imported++
				total := st.imported + st.skipped + st.failed
				st.mu.Unlock()
				progress("processing", len(entries), total, st.imported, st.skipped, st.failed)
				return nil
			},
		})
	}

	pool.Stop()
	_ = s.Sessions.UpdateStatus(sessionID, "completed")
	slog.Info("修复扫描完成",
		"library_id", lib.ID, "session_id", sessionID,
		"found", len(entries), "repaired", st.imported,
		"skipped", st.skipped, "failed", st.failed)

	session, _ := s.Sessions.GetByID(sessionID)
	return &repo.ScanResult{
		Session:  session,
		Found:    len(entries),
		Imported: st.imported,
		Skipped:  st.skipped,
		Failed:   st.failed,
	}, nil
}
