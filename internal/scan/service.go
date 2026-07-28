// 包 scan：扫描编排，将媒体采集结果写入数据库。
// 代码注释使用中文。
package scan

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"memable/internal/media"
	"memable/internal/repo"
)

// Service 扫描服务，负责遍历目录、增量判定、采集 metadata 并入库。
type Service struct {
	Sessions *repo.SessionRepo
	Media    *repo.MediaRepo
}

// Stats 扫描统计。
type Stats struct {
	SessionID string
	Found     int
	Skipped   int
	Imported  int
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
			_ = s.Sessions.UpdateStatus(sessionID, "failed")
			return nil, err
		}
		if !need {
			stats.Skipped++
			continue
		}

		m, err := s.collect(ctx, lib.ID, sessionID, e)
		if err != nil {
			_ = s.Sessions.UpdateStatus(sessionID, "failed")
			return nil, err
		}
		if err := s.Media.Upsert(m); err != nil {
			_ = s.Sessions.UpdateStatus(sessionID, "failed")
			return nil, err
		}
		stats.Imported++
	}

	if err := s.Sessions.UpdateStatus(sessionID, "completed"); err != nil {
		return nil, err
	}
	slog.Info("扫描完成", "library_id", lib.ID, "session_id", sessionID, "found", stats.Found, "imported", stats.Imported, "skipped", stats.Skipped)
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
		Kind:          e.Kind,
		RelativePath:  e.RelativePath,
		FileSize:      e.Size,
		Mtime:         e.Mtime,
		Sha1:          &sha1,
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
	case media.KindVideo:
		meta, err := media.ProbeVideo(ctx, e.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("读取视频 metadata %q: %w", e.AbsPath, err)
		}
		oshash, err := media.OSHashFile(e.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("计算视频 oHash %q: %w", e.AbsPath, err)
		}
		m.Format = &meta.Format
		m.Width = &meta.Width
		m.Height = &meta.Height
		m.DurationMs = &meta.DurationMs
		m.VideoCodec = &meta.VideoCodec
		m.AudioCodec = &meta.AudioCodec
		m.FrameRate = &meta.FrameRate
		m.BitRate = &meta.BitRate
		m.Ohash = &oshash
	}
	return m, nil
}
