// media_repo.go：media 表 CRUD 与增量判定。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"memable/internal/errx"
)

// MediaRepo media 表仓库。
type MediaRepo struct{ db *sql.DB }

func NewMediaRepo(db *sql.DB) *MediaRepo { return &MediaRepo{db: db} }

const mediaCols = `id, library_id, scan_session_id, kind, relative_path, file_size, mtime,
	format, width, height, phash, dhash, ahash, duration_ms, video_codec, audio_codec,
	frame_rate, bit_rate, oshash, sha1, thumbnail_path, created_at`

// Upsert 按 (library_id, relative_path) 插入或更新媒体记录。
func (r *MediaRepo) Upsert(m *Media) error {
	res, err := r.db.Exec(
		`INSERT INTO media (library_id, scan_session_id, kind, relative_path, file_size, mtime,
			format, width, height, phash, dhash, ahash, duration_ms, video_codec, audio_codec,
			frame_rate, bit_rate, oshash, sha1, thumbnail_path)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(library_id, relative_path) DO UPDATE SET
			scan_session_id=excluded.scan_session_id, kind=excluded.kind,
			file_size=excluded.file_size, mtime=excluded.mtime, format=excluded.format,
			width=excluded.width, height=excluded.height, phash=excluded.phash,
			dhash=excluded.dhash, ahash=excluded.ahash, duration_ms=excluded.duration_ms,
			video_codec=excluded.video_codec, audio_codec=excluded.audio_codec,
			frame_rate=excluded.frame_rate, bit_rate=excluded.bit_rate,
			oshash=excluded.oshash, sha1=excluded.sha1, thumbnail_path=excluded.thumbnail_path`,
		m.LibraryID, m.ScanSessionID, m.Kind, m.RelativePath, m.FileSize, m.Mtime,
		m.Format, m.Width, m.Height, m.Phash, m.Dhash, m.Ahash, m.DurationMs,
		m.VideoCodec, m.AudioCodec, m.FrameRate, m.BitRate, m.Oshash, m.Sha1, m.ThumbnailPath,
	)
	if err != nil {
		return errx.Wrapf(err, "写入媒体 %q", m.RelativePath)
	}
	if id, _ := res.LastInsertId(); id != 0 {
		m.ID = id
	}
	// ON CONFLICT 更新时 LastInsertId 可能不是目标行，重新读取一次确保 ID 与数据库一致。
	stored, err := r.GetByPath(m.LibraryID, m.RelativePath)
	if err != nil {
		return err
	}
	if stored != nil {
		m.ID = stored.ID
	}
	return nil
}

// GetByID 按主键查询媒体记录；不存在返回 (nil, nil)。
func (r *MediaRepo) GetByID(id int64) (*Media, error) {
	var m Media
	err := r.db.QueryRow(
		`SELECT `+mediaCols+` FROM media WHERE id = ?`,
		id,
	).Scan(&m.ID, &m.LibraryID, &m.ScanSessionID, &m.Kind, &m.RelativePath, &m.FileSize,
		&m.Mtime, &m.Format, &m.Width, &m.Height, &m.Phash, &m.Dhash, &m.Ahash,
		&m.DurationMs, &m.VideoCodec, &m.AudioCodec, &m.FrameRate, &m.BitRate,
		&m.Oshash, &m.Sha1, &m.ThumbnailPath, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.Wrapf(err, "查询媒体 id=%d", id)
	}
	return &m, nil
}

// GetByPath 按 (library_id, relative_path) 查询；不存在返回 (nil, nil)。
func (r *MediaRepo) GetByPath(libraryID int64, relPath string) (*Media, error) {
	var m Media
	err := r.db.QueryRow(
		`SELECT `+mediaCols+` FROM media WHERE library_id = ? AND relative_path = ?`,
		libraryID, relPath,
	).Scan(&m.ID, &m.LibraryID, &m.ScanSessionID, &m.Kind, &m.RelativePath, &m.FileSize,
		&m.Mtime, &m.Format, &m.Width, &m.Height, &m.Phash, &m.Dhash, &m.Ahash,
		&m.DurationMs, &m.VideoCodec, &m.AudioCodec, &m.FrameRate, &m.BitRate,
		&m.Oshash, &m.Sha1, &m.ThumbnailPath, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.Wrapf(err, "查询媒体 lib=%d path=%q", libraryID, relPath)
	}
	return &m, nil
}

// NeedScan 增量判定：记录不存在，或 mtime/file_size 任一变化时需要重新扫描。
func (r *MediaRepo) NeedScan(libraryID int64, relPath string, mtime time.Time, size int64) (bool, error) {
	m, err := r.GetByPath(libraryID, relPath)
	if err != nil {
		return false, err
	}
	if m == nil {
		return true, nil // 新文件
	}
	if m.FileSize != size || !m.Mtime.Equal(mtime) {
		return true, nil // 已修改
	}
	return false, nil // 未变化，跳过
}

// NeedRepair 检查记录是否需要修补（缺失元数据或缩略图）。
// 新文件或已有记录中 phash/尺寸/缩略图 任一项缺失时返回 true。
func (r *MediaRepo) NeedRepair(libraryID int64, relPath string, kind string) (bool, error) {
	m, err := r.GetByPath(libraryID, relPath)
	if err != nil {
		return false, err
	}
	if m == nil {
		return true, nil // 新文件，需要采集
	}
	switch kind {
	case "image":
		if m.Phash == nil || m.Width == nil || m.Height == nil {
			return true, nil
		}
	case "video":
		if m.Phash == nil || m.Oshash == nil || m.Width == nil || m.Height == nil || m.DurationMs == nil {
			return true, nil
		}
	}
	if m.ThumbnailPath == nil {
		return true, nil
	}
	return false, nil // 元数据完整，跳过
}

// FindBySha1 按 SHA1 查询完全相同文件。
func (r *MediaRepo) FindBySha1(sha1 string) ([]Media, error) {
	return r.query(`SELECT `+mediaCols+` FROM media WHERE sha1 = ?`, sha1)
}

// ListByLibrary 列出库下全部媒体。
func (r *MediaRepo) ListByLibrary(libraryID int64) ([]Media, error) {
	return r.query(`SELECT `+mediaCols+` FROM media WHERE library_id = ? ORDER BY relative_path`, libraryID)
}

// ListBySession 列出某次扫描会话的媒体。
func (r *MediaRepo) ListBySession(sessionID string) ([]Media, error) {
	return r.query(`SELECT `+mediaCols+` FROM media WHERE scan_session_id = ? ORDER BY relative_path`, sessionID)
}

// SearchBySha1 按 SHA1 精确搜索。
func (r *MediaRepo) SearchBySha1(sha1 string) ([]Media, error) {
	return r.FindBySha1(strings.ToLower(strings.TrimSpace(sha1)))
}

// ListByKind 列出指定类型的全部正式媒体。
func (r *MediaRepo) ListByKind(kind string) ([]Media, error) {
	cols := `m.` + strings.ReplaceAll(mediaCols, ", ", ", m.")
	return r.query(`SELECT `+cols+` FROM media m
		LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE m.kind = ? AND COALESCE(s.is_temporary, 0) = 0 ORDER BY m.library_id, m.relative_path`, kind)
}

// ListAllFormal 列出全部正式媒体（排除临时扫描），用于重复报告。
func (r *MediaRepo) ListAllFormal() ([]Media, error) {
	cols := `m.` + strings.ReplaceAll(mediaCols, ", ", ", m.")
	return r.query(`SELECT ` + cols + ` FROM media m
		LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE COALESCE(s.is_temporary, 0) = 0 ORDER BY m.library_id, m.relative_path`)
}

// ListByDirectory 列出库下指定目录内的全部媒体。
func (r *MediaRepo) ListByDirectory(libraryID int64, relDir string) ([]Media, error) {
	prefix := strings.TrimPrefix(strings.TrimPrefix(relDir, "/"), "\\")
	if prefix != "" {
		prefix = strings.ReplaceAll(prefix, "\\", "/") + "/"
	}
	return r.query(
		`SELECT `+mediaCols+` FROM media WHERE library_id = ? AND relative_path LIKE ? ORDER BY relative_path`,
		libraryID, prefix+"%",
	)
}

// ListByDirectoryDirect 列出库下指定目录的直属媒体（不含子目录文件）。
func (r *MediaRepo) ListByDirectoryDirect(libraryID int64, relDir string) ([]Media, error) {
	prefix := strings.TrimPrefix(strings.TrimPrefix(relDir, "/"), "\\")
	if prefix != "" {
		prefix = strings.ReplaceAll(prefix, "\\", "/") + "/"
	}
	// 匹配 prefix 后不再含有 / 的路径，即直属文件
	return r.query(
		`SELECT `+mediaCols+` FROM media WHERE library_id = ? AND relative_path LIKE ? AND relative_path NOT LIKE ? ORDER BY relative_path`,
		libraryID, prefix+"%", prefix+"%/%",
	)
}

// DeleteByDirectory 删除库下指定目录及其子目录的所有媒体记录，
// 返回删除数量与被删除记录的缩略图引用。
func (r *MediaRepo) DeleteByDirectory(libraryID int64, relDir string) (int, []ThumbRef, error) {
	prefix := strings.TrimPrefix(strings.TrimPrefix(relDir, "/"), "\\")
	if prefix != "" {
		prefix = strings.ReplaceAll(prefix, "\\", "/") + "/"
	}
	var thumbRefs []ThumbRef
	deleted := 0
	err := WithTx(r.db, 3, func(tx *sql.Tx) error {
		thumbRefs = thumbRefs[:0]
		// 收集待删除记录的缩略图路径
		rows, err := tx.Query(
			`SELECT DISTINCT kind, thumbnail_path FROM media
			 WHERE library_id = ? AND thumbnail_path IS NOT NULL AND thumbnail_path <> ''
			 AND (relative_path LIKE ? OR relative_path LIKE ?)`,
			libraryID, prefix+"%", prefix+"%/%",
		)
		if err != nil {
			return errx.Wrapf(err, "查询目录 %q 的缩略图", relDir)
		}
		for rows.Next() {
			var kind string
			var tp string
			if err := rows.Scan(&kind, &tp); err != nil {
				rows.Close()
				return errx.Wrapf(err, "扫描缩略图路径")
			}
			thumbRefs = append(thumbRefs, ThumbRef{Kind: kind, Rel: tp})
		}
		if err := rows.Close(); err != nil {
			return errx.Wrapf(err, "关闭缩略图查询")
		}
		if err := rows.Err(); err != nil {
			return errx.Wrapf(err, "遍历缩略图")
		}

		// 删除媒体记录
		res, err := tx.Exec(
			`DELETE FROM media WHERE library_id = ? AND (relative_path LIKE ? OR relative_path LIKE ?)`,
			libraryID, prefix+"%", prefix+"%/%",
		)
		if err != nil {
			return errx.Wrapf(err, "删除目录 %q 的媒体记录", relDir)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			deleted = int(n)
		}
		return nil
	})
	return deleted, thumbRefs, err
}

// SearchByPath 全路径模糊搜索（拼接 library.path 后匹配）。
func (r *MediaRepo) SearchByPath(pattern string) ([]Media, error) {
	// JOIN 查询需为列加 m. 前缀避免歧义
	cols := `m.` + strings.ReplaceAll(mediaCols, ", ", ", m.")
	return r.query(
		`SELECT `+cols+` FROM media m JOIN libraries l ON l.id = m.library_id
		 WHERE l.path || m.relative_path LIKE ? ESCAPE '\' ORDER BY m.relative_path`,
		"%"+escapeLike(pattern)+"%",
	)
}

// Delete 删除媒体记录；物理缩略图由上层处理。
func (r *MediaRepo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM media WHERE id = ?`, id)
	if err != nil {
		return errx.Wrapf(err, "删除媒体 id=%d", id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("媒体 id=%d 不存在", id)
	}
	return nil
}

// DeleteByIDs 在同一事务中删除媒体记录，返回可能需要清理的缩略图引用。
func (r *MediaRepo) DeleteByIDs(ids []int64) ([]ThumbRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	thumbRefs := make([]ThumbRef, 0)
	err := WithTx(r.db, 3, func(tx *sql.Tx) error {
		for _, id := range ids {
			var kind string
			var thumb sql.NullString
			err := tx.QueryRow(`SELECT kind, thumbnail_path FROM media WHERE id = ?`, id).Scan(&kind, &thumb)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return fmt.Errorf("查询待删除媒体 id=%d: %w", id, err)
			}
			if thumb.Valid && thumb.String != "" {
				thumbRefs = append(thumbRefs, ThumbRef{Kind: kind, Rel: thumb.String})
			}
			if _, err := tx.Exec(`DELETE FROM media WHERE id = ?`, id); err != nil {
				return fmt.Errorf("删除媒体 id=%d: %w", id, err)
			}
		}
		return nil
	})
	return thumbRefs, errx.Wrapf(err, "批量删除媒体")
}

// CountThumbnailReferences 统计仍引用指定缩略图路径的媒体记录数。
func (r *MediaRepo) CountThumbnailReferences(thumbPath string) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM media WHERE thumbnail_path = ?`, thumbPath,
	).Scan(&n)
	return n, errx.Wrapf(err, "统计缩略图 %q 引用数", thumbPath)
}

// UpdateThumbnailPath 更新媒体的缩略图路径。
func (r *MediaRepo) UpdateThumbnailPath(id int64, thumbPath *string) error {
	_, err := r.db.Exec(`UPDATE media SET thumbnail_path = ? WHERE id = ?`, thumbPath, id)
	return errx.Wrapf(err, "更新缩略图路径 id=%d", id)
}

// UpdateLibrary 更新媒体的归属库（临时扫描入库时使用）。
func (r *MediaRepo) UpdateLibrary(id int64, libraryID int64) error {
	_, err := r.db.Exec(`UPDATE media SET library_id = ? WHERE id = ?`, libraryID, id)
	return errx.Wrapf(err, "更新媒体库归属 id=%d", id)
}

// query 通用多行查询。
func (r *MediaRepo) query(q string, args ...any) ([]Media, error) {
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, errx.Wrapf(err, "媒体查询")
	}
	defer rows.Close()

	out := make([]Media, 0)
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.LibraryID, &m.ScanSessionID, &m.Kind, &m.RelativePath,
			&m.FileSize, &m.Mtime, &m.Format, &m.Width, &m.Height, &m.Phash, &m.Dhash,
			&m.Ahash, &m.DurationMs, &m.VideoCodec, &m.AudioCodec, &m.FrameRate,
			&m.BitRate, &m.Oshash, &m.Sha1, &m.ThumbnailPath, &m.CreatedAt); err != nil {
			return nil, errx.Wrapf(err, "扫描媒体行")
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	repl := map[rune]string{'%': `\%`, '_': `\_`, '\\': `\\`}
	var b []rune
	for _, c := range s {
		if r, ok := repl[c]; ok {
			b = append(b, []rune(r)...)
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}
