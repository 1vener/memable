// media_repo.go：media 表 CRUD 与增量判定。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"memable/internal/errx"
)

// MediaRepo media 表仓库。
type MediaRepo struct{ db *sql.DB }

func NewMediaRepo(db *sql.DB) *MediaRepo { return &MediaRepo{db: db} }

const mediaCols = `id, library_id, scan_session_id, kind, relative_path, file_size, mtime,
	format, width, height, phash, dhash, ahash, duration_ms, video_codec, audio_codec,
	frame_rate, bit_rate, oshash, sha1, thumbnail_path, cover_phash, created_at`

// Upsert 按 (library_id, relative_path) 插入或更新媒体记录。
// 使用 RETURNING id 一步拿到目标行 ID，避免 INSERT 后再查一次全行。
func (r *MediaRepo) Upsert(m *Media) error {
	err := r.db.QueryRow(
		`INSERT INTO media (library_id, scan_session_id, kind, relative_path, file_size, mtime,
			format, width, height, phash, dhash, ahash, duration_ms, video_codec, audio_codec,
			frame_rate, bit_rate, oshash, sha1, thumbnail_path, cover_phash)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(library_id, relative_path) DO UPDATE SET
			scan_session_id=excluded.scan_session_id, kind=excluded.kind,
			file_size=excluded.file_size, mtime=excluded.mtime, format=excluded.format,
			width=excluded.width, height=excluded.height, phash=excluded.phash,
			dhash=excluded.dhash, ahash=excluded.ahash, duration_ms=excluded.duration_ms,
			video_codec=excluded.video_codec, audio_codec=excluded.audio_codec,
			frame_rate=excluded.frame_rate, bit_rate=excluded.bit_rate,
			oshash=excluded.oshash, sha1=excluded.sha1, thumbnail_path=excluded.thumbnail_path,
			cover_phash=excluded.cover_phash,
			created_at=datetime('now')
		 RETURNING id`,
		m.LibraryID, m.ScanSessionID, m.Kind, m.RelativePath, m.FileSize, m.Mtime,
		m.Format, m.Width, m.Height, m.Phash, m.Dhash, m.Ahash, m.DurationMs,
		m.VideoCodec, m.AudioCodec, m.FrameRate, m.BitRate, m.Oshash, m.Sha1, m.ThumbnailPath,
		m.CoverPHash,
	).Scan(&m.ID)
	if err != nil {
		return errx.Wrapf(err, "写入媒体 %q", m.RelativePath)
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
		&m.Oshash, &m.Sha1, &m.ThumbnailPath, &m.CoverPHash, &m.CreatedAt)
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
		&m.Oshash, &m.Sha1, &m.ThumbnailPath, &m.CoverPHash, &m.CreatedAt)
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

// ListFormalPage 按修改时间倒序列出正式媒体分页。
func (r *MediaRepo) ListFormalPage(kind string, page, pageSize int) (MediaPage, error) {
	cols := `m.` + strings.ReplaceAll(mediaCols, ", ", ", m.")
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM media m
		LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE m.kind = ? AND COALESCE(s.is_temporary, 0) = 0`, kind).Scan(&total); err != nil {
		return MediaPage{}, errx.Wrapf(err, "统计媒体分页总数")
	}
	items, err := r.query(`SELECT `+cols+` FROM media m
		LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE m.kind = ? AND COALESCE(s.is_temporary, 0) = 0
		ORDER BY m.mtime DESC, m.id DESC LIMIT ? OFFSET ?`, kind, pageSize, (page-1)*pageSize)
	if err != nil {
		return MediaPage{}, err
	}
	totalPages := (total + pageSize - 1) / pageSize
	return MediaPage{Total: total, Page: page, PageSize: pageSize, TotalPages: totalPages, Items: items}, nil
}

// groupKey 目录分组键（收藏库 + 前 depth 段相对目录）。
type groupKey struct {
	libraryID int64
	path      string
}

// groupData 分组聚合数据。
type groupData struct {
	group MediaGroup
}

// groupKeyOf 计算媒体相对路径所属的分组键（取前 depth 段，根目录为空串）。
func groupKeyOf(depth int, libraryID int64, relativePath string) groupKey {
	parts := strings.Split(strings.ReplaceAll(relativePath, "\\", "/"), "/")
	dirParts := make([]string, 0, len(parts))
	if len(parts) > 1 {
		dirParts = append(dirParts, parts[:len(parts)-1]...)
	}
	if depth < len(dirParts) {
		dirParts = dirParts[:depth]
	}
	return groupKey{libraryID: libraryID, path: strings.Join(dirParts, "/")}
}

// aggregateFormalGroups 聚合正式媒体（排除临时扫描）为目录分组，
// 返回分组 map 与按最新 mtime 倒序的分组键顺序。
func (r *MediaRepo) aggregateFormalGroups(depth int) (map[groupKey]*groupData, []groupKey, error) {
	rows, err := r.db.Query(`SELECT m.library_id, l.name, m.relative_path, m.mtime FROM media m
		JOIN libraries l ON l.id = m.library_id
		LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE COALESCE(s.is_temporary, 0) = 0`)
	if err != nil {
		return nil, nil, errx.Wrapf(err, "查询正式媒体目录分组")
	}
	defer rows.Close()
	groups := make(map[groupKey]*groupData)
	order := make([]groupKey, 0)
	for rows.Next() {
		var libraryID int64
		var name, relativePath string
		var mtime time.Time
		if err := rows.Scan(&libraryID, &name, &relativePath, &mtime); err != nil {
			return nil, nil, errx.Wrapf(err, "扫描正式媒体目录分组")
		}
		k := groupKeyOf(depth, libraryID, relativePath)
		g := groups[k]
		if g == nil {
			g = &groupData{group: MediaGroup{LibraryID: libraryID, LibraryName: name, GroupPath: k.path, Items: make([]Media, 0)}}
			groups[k] = g
			order = append(order, k)
		}
		g.group.Total++
		if g.group.Total == 1 || mtime.After(g.group.LatestMtime) {
			g.group.LatestMtime = mtime
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, errx.Wrapf(err, "遍历正式媒体目录分组")
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]].group, groups[order[j]].group
		if !a.LatestMtime.Equal(b.LatestMtime) {
			return a.LatestMtime.After(b.LatestMtime)
		}
		if a.LibraryID != b.LibraryID {
			return a.LibraryID < b.LibraryID
		}
		return a.GroupPath < b.GroupPath
	})
	return groups, order, nil
}

// loadGroupItems 查询分组（库 + 路径前缀）的全部后代媒体，按 mtime 倒序。
func (r *MediaRepo) loadGroupItems(k groupKey) ([]Media, error) {
	cols := `m.` + strings.ReplaceAll(mediaCols, ", ", ", m.")
	var query string
	var args []any
	if k.path == "" {
		query = `SELECT ` + cols + ` FROM media m LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
			WHERE m.library_id = ? AND COALESCE(s.is_temporary, 0) = 0 AND m.relative_path NOT LIKE '%/%'
			ORDER BY m.mtime DESC, m.id DESC`
		args = []any{k.libraryID}
	} else {
		query = `SELECT ` + cols + ` FROM media m LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
			WHERE m.library_id = ? AND COALESCE(s.is_temporary, 0) = 0
			AND (m.relative_path = ? OR m.relative_path LIKE ? ESCAPE '\')
			ORDER BY m.mtime DESC, m.id DESC`
		args = []any{k.libraryID, k.path, escapeLike(k.path) + "/%"}
	}
	return r.query(query, args...)
}

// groupPageSlice 按 offset/limit 切组并装载整组媒体，返回切片与组总数。
func (r *MediaRepo) groupPageSlice(groups map[groupKey]*groupData, order []groupKey, offset, limit int) ([]MediaGroup, int, error) {
	if offset > len(order) {
		offset = len(order)
	}
	end := offset + limit
	if end > len(order) {
		end = len(order)
	}
	for _, k := range order[offset:end] {
		items, err := r.loadGroupItems(k)
		if err != nil {
			return nil, 0, err
		}
		groups[k].group.Items = items
	}
	out := make([]MediaGroup, 0, end-offset)
	for _, k := range order[offset:end] {
		out = append(out, groups[k].group)
	}
	return out, len(order), nil
}

// ListFormalGroups 按目录前 depth 段聚合正式媒体，并只对组分页。
func (r *MediaRepo) ListFormalGroups(depth, offset, limit int) ([]MediaGroup, int, error) {
	groups, order, err := r.aggregateFormalGroups(depth)
	if err != nil {
		return nil, 0, err
	}
	return r.groupPageSlice(groups, order, offset, limit)
}

// ListFormalGroupsByQuery 按文件名（含路径，大小写不敏感）搜索正式媒体，
// 返回命中文件所属目录分组（整组后代媒体，按 mtime 倒序）。
// 命中多个分组时同时返回；total 为命中组数。q 为空时回退全量分组。
func (r *MediaRepo) ListFormalGroupsByQuery(depth int, q string, offset, limit int) ([]MediaGroup, int, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return r.ListFormalGroups(depth, offset, limit)
	}
	// 1. 命中媒体 → 命中组键集合
	rows, err := r.db.Query(`SELECT m.library_id, m.relative_path FROM media m
		LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE COALESCE(s.is_temporary, 0) = 0 AND m.relative_path LIKE ? ESCAPE '\'`,
		"%"+escapeLike(q)+"%")
	if err != nil {
		return nil, 0, errx.Wrapf(err, "查询命中媒体")
	}
	hitKeys := make(map[groupKey]struct{})
	for rows.Next() {
		var libraryID int64
		var relativePath string
		if err := rows.Scan(&libraryID, &relativePath); err != nil {
			rows.Close()
			return nil, 0, errx.Wrapf(err, "扫描命中媒体")
		}
		hitKeys[groupKeyOf(depth, libraryID, relativePath)] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return nil, 0, errx.Wrapf(err, "关闭命中媒体查询")
	}
	if err := rows.Err(); err != nil {
		return nil, 0, errx.Wrapf(err, "遍历命中媒体")
	}

	// 2. 聚合全部分组，过滤出命中组，分页返回
	groups, order, err := r.aggregateFormalGroups(depth)
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]groupKey, 0, len(hitKeys))
	for _, k := range order {
		if _, ok := hitKeys[k]; ok {
			filtered = append(filtered, k)
		}
	}
	return r.groupPageSlice(groups, filtered, offset, limit)
}

// FormalStatistics 汇总正式媒体数量、大小和视频时长。
func (r *MediaRepo) FormalStatistics() (MediaStatistics, error) {
	var out MediaStatistics
	rows, err := r.db.Query(`SELECT kind, COUNT(*), COALESCE(SUM(file_size),0), COALESCE(SUM(duration_ms),0)
		FROM media m LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		WHERE COALESCE(s.is_temporary, 0) = 0 GROUP BY kind`)
	if err != nil {
		return out, errx.Wrapf(err, "统计正式媒体")
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count, size, durationMs int64
		if err := rows.Scan(&kind, &count, &size, &durationMs); err != nil {
			return out, errx.Wrapf(err, "读取正式媒体统计")
		}
		out.TotalSize += size
		if kind == "image" {
			out.Image = MediaKindStatistics{Count: count, Size: size}
		} else if kind == "video" {
			out.Video = VideoStatistics{Count: count, Size: size, DurationMs: durationMs}
		}
	}
	return out, rows.Err()
}

// RenameDirectoryPrefix 批量更新库下指定目录及其子目录下所有媒体的相对路径前缀
// （目录重命名/移动时使用），返回受影响行数。
// 匹配用 substr 精确目录边界：relative_path 等于 oldPrefix，或以其 + "/" 开头；
// 不使用 LIKE，避免 %/_ 通配符转义与 oldPrefix 为另一目录前缀（如 a/bc）时的误伤。
func (r *MediaRepo) RenameDirectoryPrefix(libraryID int64, oldPrefix, newPrefix string) (int, error) {
	oldPrefix = strings.ReplaceAll(oldPrefix, "\\", "/")
	newPrefix = strings.ReplaceAll(newPrefix, "\\", "/")
	oldPrefix = strings.Trim(oldPrefix, "/")
	newPrefix = strings.Trim(newPrefix, "/")
	if oldPrefix == "" {
		return 0, fmt.Errorf("旧目录前缀不能为空")
	}
	res, err := r.db.Exec(
		`UPDATE media SET relative_path = ? || substr(relative_path, ?)
		 WHERE library_id = ? AND substr(relative_path, 1, ?) = ?
		   AND (length(relative_path) = ? OR substr(relative_path, ? + 1, 1) = '/')`,
		newPrefix, utf8.RuneCountInString(oldPrefix)+1,
		libraryID, utf8.RuneCountInString(oldPrefix), oldPrefix,
		utf8.RuneCountInString(oldPrefix), utf8.RuneCountInString(oldPrefix),
	)
	if err != nil {
		return 0, errx.Wrapf(err, "更新目录前缀 %q -> %q", oldPrefix, newPrefix)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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

// DirChild 库目录树的单个子目录节点。
type DirChild struct {
	Name        string
	Path        string
	HasChildren bool
}

// ListDirChildren 返回库下指定目录的直属子目录，由 media.relative_path 派生，
// 不读本地磁盘。目录改名/移动/删除、媒体增删后自动反映数据库真值；
// 仅包含已入库媒体的目录（空目录、只有非媒体文件的目录不出现）。
// 查询走 UNIQUE(library_id, relative_path) 索引做区间扫描，逐层懒加载。
func (r *MediaRepo) ListDirChildren(libraryID int64, relDir string) ([]DirChild, error) {
	parent := strings.TrimPrefix(strings.TrimPrefix(relDir, "/"), "\\")
	parent = strings.Trim(strings.ReplaceAll(parent, "\\", "/"), "/")
	pattern := "%"
	if parent != "" {
		pattern = parent + "/%"
	}
	// substr(relative_path, offset) 取 parent 之后的部分：offset = 字符数(parent)+2
	// （substr 为 1 基，跳过 parent 与其后的 "/"）；根目录 offset=1 取整条路径。
	// 注意必须用 RuneCountInString（字符数）而非 len（字节数）：SQLite 的
	// substr/instr/length 一律按字符计，中文/emoji 目录名按字节算会切错位置。
	// 内层 INSTR>0 排除直属文件（无斜杠，防根级文件被误判为目录）；
	// 外层 MAX(has_more) 判定该子目录下是否还有更深层级（has_children）。
	offset := utf8.RuneCountInString(parent) + 2
	if parent == "" {
		offset = 1
	}
	rows, err := r.db.Query(
		`SELECT child, MAX(has_more) AS has_children FROM (
			SELECT
				SUBSTR(s, 1, INSTR(s, '/') - 1) AS child,
				(INSTR(SUBSTR(s, INSTR(s, '/') + 1), '/') > 0) AS has_more
			FROM (
				SELECT substr(relative_path, ?) AS s
				FROM media
				WHERE library_id = ? AND relative_path LIKE ?
				  AND INSTR(substr(relative_path, ?), '/') > 0
			)
		) WHERE child <> '' GROUP BY child ORDER BY child`,
		offset, libraryID, pattern, offset,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "查询目录子项 %q", parent)
	}
	defer rows.Close()

	children := make([]DirChild, 0)
	for rows.Next() {
		var name string
		var hasChildren bool
		if err := rows.Scan(&name, &hasChildren); err != nil {
			return nil, errx.Wrapf(err, "读取目录子项 %q", parent)
		}
		path := name
		if parent != "" {
			path = parent + "/" + name
		}
		children = append(children, DirChild{Name: name, Path: path, HasChildren: hasChildren})
	}
	return children, rows.Err()
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

// LibrarySearchHit 库内文件名/目录名搜索的目录级命中条目。
type LibrarySearchHit struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	DirPath     string `json:"dir_path"`    // 相对目录路径（''=库根）
	DirName     string `json:"dir_name"`    // 目录名（库根为 ''）
	MatchType   string `json:"match_type"`  // "dir"=目录名命中 / "file"=文件名命中（汇总父目录）
	MatchCount  int    `json:"match_count"` // file 类型：命中文件数
}

// SearchLibraries 跨全部正式收藏库搜索文件名/目录名，返回目录级命中（对齐
// Windows 文件搜索语义）：
//   - 目录名命中 → 直接返回该目录（match_type=dir）；
//   - 文件名命中 → 汇总到其父目录，只返回父目录（match_type=file，match_count=命中文件数）。
//
// 先按 relative_path LIKE 粗筛（LIMIT 限制原始行数），再在 Go 侧逐段判定并聚合。
func (r *MediaRepo) SearchLibraries(query string, limit int) ([]LibrarySearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []LibrarySearchHit{}, nil
	}
	if limit <= 0 {
		limit = 2000
	}
	rows, err := r.db.Query(
		`SELECT m.library_id, m.relative_path, l.name
		 FROM media m
		 JOIN libraries l ON l.id = m.library_id
		 LEFT JOIN scan_sessions s ON s.id = m.scan_session_id
		 WHERE COALESCE(s.is_temporary, 0) = 0
		   AND m.relative_path LIKE ? ESCAPE '\'
		 ORDER BY m.library_id, m.relative_path
		 LIMIT ?`,
		"%"+escapeLike(query)+"%", limit,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "搜索文件名/目录名 %q", query)
	}
	defer rows.Close()

	// 目录命中 map：key=(libID|dirPath)；文件命中 map：key=(libID|dirPath) -> 计数
	type key struct {
		libID int64
		dir   string
	}
	dirHits := make(map[key]string) // key -> 库名
	fileHits := make(map[key]int)   // key -> 命中文件数
	libNames := make(map[int64]string)
	q := strings.ToLower(query)

	for rows.Next() {
		var libID int64
		var relPath, libName string
		if err := rows.Scan(&libID, &relPath, &libName); err != nil {
			return nil, errx.Wrapf(err, "扫描搜索结果行")
		}
		libNames[libID] = libName
		parts := strings.Split(relPath, "/")
		for i, seg := range parts {
			if !strings.Contains(strings.ToLower(seg), q) {
				continue
			}
			if i == len(parts)-1 {
				// 文件名命中：汇总父目录（父目录可为空 = 库根）
				parent := ""
				if len(parts) > 1 {
					parent = strings.Join(parts[:len(parts)-1], "/")
				}
				fileHits[key{libID, parent}]++
			} else {
				// 目录名命中：直接返回该目录
				dir := strings.Join(parts[:i+1], "/")
				dirHits[key{libID, dir}] = libName
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errx.Wrapf(err, "遍历搜索结果行")
	}

	out := make([]LibrarySearchHit, 0, len(dirHits)+len(fileHits))
	// 目录名命中优先；同一目录若同时命中文件名，以 dir 类型展示（更具体）
	for k, name := range dirHits {
		out = append(out, LibrarySearchHit{
			LibraryID:   k.libID,
			LibraryName: name,
			DirPath:     k.dir,
			DirName:     dirNameOf(k.dir),
			MatchType:   "dir",
			MatchCount:  fileHits[k],
		})
		delete(fileHits, k)
	}
	for k, n := range fileHits {
		out = append(out, LibrarySearchHit{
			LibraryID:   k.libID,
			LibraryName: libNames[k.libID],
			DirPath:     k.dir,
			DirName:     dirNameOf(k.dir),
			MatchType:   "file",
			MatchCount:  n,
		})
	}
	// 排序：库名 → 目录路径（目录命中在前）
	sort.Slice(out, func(i, j int) bool {
		if out[i].LibraryName != out[j].LibraryName {
			return out[i].LibraryName < out[j].LibraryName
		}
		if out[i].MatchType != out[j].MatchType {
			return out[i].MatchType < out[j].MatchType // "dir" < "file"
		}
		return out[i].DirPath < out[j].DirPath
	})
	return out, nil
}

// dirNameOf 返回目录路径的最后一段（库根返回空串）。
func dirNameOf(dir string) string {
	if dir == "" {
		return ""
	}
	parts := strings.Split(dir, "/")
	return parts[len(parts)-1]
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

// UpdateSha1 更新媒体记录的 SHA1（补齐 SHA1 任务使用）。
// 写入前统一转为小写并去除首尾空白：与本地扫描生成的 sha1
// （hex.EncodeToString 小写）保持一致，避免同一文件因大小写不同
// 无法被 groupBySha1 精确分组（重复检测漏报）。
func (r *MediaRepo) UpdateSha1(id int64, sha1 string) error {
	sha1 = strings.ToLower(strings.TrimSpace(sha1))
	if _, err := r.db.Exec(`UPDATE media SET sha1 = ? WHERE id = ?`, sha1, id); err != nil {
		return errx.Wrapf(err, "更新媒体 sha1 id=%d", id)
	}
	return nil
}

// ListMissingSha1 返回收藏库中 sha1 缺失的媒体记录（id + 相对路径）。
func (r *MediaRepo) ListMissingSha1(libraryID int64) ([]Media, error) {
	rows, err := r.db.Query(
		`SELECT id, relative_path FROM media WHERE library_id = ? AND sha1 IS NULL`,
		libraryID,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "查询缺失 sha1 媒体 lib=%d", libraryID)
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.RelativePath); err != nil {
			return nil, errx.Wrapf(err, "读取缺失 sha1 媒体 lib=%d", libraryID)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, errx.Wrapf(err, "遍历缺失 sha1 媒体 lib=%d", libraryID)
	}
	return out, nil
}

// ListMissingSha1ByDirectory 返回收藏库指定目录（含子目录）下 sha1 缺失的媒体记录。
// 供 115 网盘补齐 SHA1 任务使用：仅匹配目录内记录，避免全库扫描；
// 需带 file_size（大小一致性校验）。
func (r *MediaRepo) ListMissingSha1ByDirectory(libraryID int64, relDir string) ([]Media, error) {
	prefix := strings.TrimPrefix(strings.TrimPrefix(relDir, "/"), "\\")
	if prefix != "" {
		prefix = strings.ReplaceAll(prefix, "\\", "/") + "/"
	}
	rows, err := r.db.Query(
		`SELECT id, relative_path, file_size FROM media
		 WHERE library_id = ? AND sha1 IS NULL AND relative_path LIKE ? ORDER BY relative_path`,
		libraryID, prefix+"%",
	)
	if err != nil {
		return nil, errx.Wrapf(err, "查询目录缺失 sha1 媒体 lib=%d dir=%q", libraryID, relDir)
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.RelativePath, &m.FileSize); err != nil {
			return nil, errx.Wrapf(err, "读取目录缺失 sha1 媒体 lib=%d", libraryID)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, errx.Wrapf(err, "遍历目录缺失 sha1 媒体 lib=%d", libraryID)
	}
	return out, nil
}

// UpdateLibrary 更新媒体的归属库（临时扫描入库时使用）。
func (r *MediaRepo) UpdateLibrary(id int64, libraryID int64) error {
	_, err := r.db.Exec(`UPDATE media SET library_id = ? WHERE id = ?`, libraryID, id)
	return errx.Wrapf(err, "更新媒体库归属 id=%d", id)
}

// UpdateLibraryAndPath 更新媒体归属库与相对路径（移动到目标库指定目录时使用）。
func (r *MediaRepo) UpdateLibraryAndPath(id int64, libraryID int64, newRelPath string) error {
	_, err := r.db.Exec(
		`UPDATE media SET library_id = ?, relative_path = ? WHERE id = ?`,
		libraryID, newRelPath, id,
	)
	return errx.Wrapf(err, "更新媒体归属与路径 id=%d", id)
}

// HasAnyRelativePath 判断目标库是否已存在任一相对路径（冲突检测用）。
func (r *MediaRepo) HasAnyRelativePath(libraryID int64, relPaths []string) (bool, error) {
	if len(relPaths) == 0 {
		return false, nil
	}
	placeholders := make([]string, len(relPaths))
	args := make([]any, 0, len(relPaths)+1)
	args = append(args, libraryID)
	for i, p := range relPaths {
		placeholders[i] = "?"
		args = append(args, p)
	}
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM media WHERE library_id = ? AND relative_path IN (`+
			strings.Join(placeholders, ",")+`)`, args...,
	).Scan(&n)
	if err != nil {
		return false, errx.Wrapf(err, "检查相对路径冲突")
	}
	return n > 0, nil
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
			&m.BitRate, &m.Oshash, &m.Sha1, &m.ThumbnailPath, &m.CoverPHash, &m.CreatedAt); err != nil {
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
