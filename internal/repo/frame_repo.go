// frame_repo.go：video_frames 表 CRUD。
// 代码注释使用中文。
package repo

import (
	"database/sql"

	"memable/internal/errx"
)

// FrameRepo video_frames 表仓库。
type FrameRepo struct{ db *sql.DB }

func NewFrameRepo(db *sql.DB) *FrameRepo { return &FrameRepo{db: db} }

// Upsert 按 (media_id, frame_index) 插入或更新关键帧。
func (r *FrameRepo) Upsert(f *VideoFrame) error {
	res, err := r.db.Exec(
		`INSERT INTO video_frames (media_id, frame_index, sample_ratio, time_ms, phash, image_path)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(media_id, frame_index) DO UPDATE SET
			sample_ratio=excluded.sample_ratio, time_ms=excluded.time_ms,
			phash=excluded.phash, image_path=excluded.image_path`,
		f.MediaID, f.FrameIndex, f.SampleRatio, f.TimeMs, f.Phash, f.ImagePath,
	)
	if err != nil {
		return errx.Wrapf(err, "写入关键帧 media=%d index=%d", f.MediaID, f.FrameIndex)
	}
	if f.ID == 0 {
		f.ID, _ = res.LastInsertId()
	}
	return nil
}

// ListByMedia 按帧序号升序列出某视频的全部关键帧。
func (r *FrameRepo) ListByMedia(mediaID int64) ([]VideoFrame, error) {
	rows, err := r.db.Query(
		`SELECT id, media_id, frame_index, sample_ratio, time_ms, phash, image_path
		 FROM video_frames WHERE media_id = ? ORDER BY frame_index`, mediaID,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "查询关键帧 media=%d", mediaID)
	}
	defer rows.Close()

	var out []VideoFrame
	for rows.Next() {
		var f VideoFrame
		if err := rows.Scan(&f.ID, &f.MediaID, &f.FrameIndex, &f.SampleRatio, &f.TimeMs, &f.Phash, &f.ImagePath); err != nil {
			return nil, errx.Wrapf(err, "扫描关键帧行")
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteByMedia 删除某视频全部关键帧（重抽帧前清理）。
func (r *FrameRepo) DeleteByMedia(mediaID int64) error {
	_, err := r.db.Exec(`DELETE FROM video_frames WHERE media_id = ?`, mediaID)
	return errx.Wrapf(err, "删除关键帧 media=%d", mediaID)
}
