// media_repo.go：media 表 CRUD 与增量判定。
// 代码注释使用中文。
package repo

import (
	"database/sql"
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

// Delete 删除媒体记录；video_frames 外键级联删除（物理缩略图由上层处理）。
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

	var out []Media
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
