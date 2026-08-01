// duplicate_repo.go：重复报告三张表（duplicate_reports/groups/members）的仓库。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"strings"
	"time"

	"memable/internal/errx"
)

// DuplicateReport 重复报告主表记录。
type DuplicateReport struct {
	ID                  int64     `json:"id"`
	BackgroundTaskID    *string   `json:"background_task_id,omitempty"`
	Scope               string    `json:"scope"`
	MediaType           string    `json:"media_type"`
	ImageThreshold      int       `json:"image_threshold"`
	VideoPhashDistance  int       `json:"video_phash_distance"`
	VideoDurationDiffMs int64     `json:"video_duration_diff_ms"`
	OshashFilter        bool      `json:"oshash_filter"`
	IncludeSHA1         bool      `json:"include_sha1"`
	Stale               bool      `json:"stale"`
	TotalGroups         int       `json:"total_groups"`
	TotalFiles          int       `json:"total_files"`
	CreatedAt           time.Time `json:"created_at"`
}

// DuplicateGroup 重复组记录。
type DuplicateGroup struct {
	ID        int64  `json:"id"`
	ReportID  int64  `json:"report_id"`
	GroupType string `json:"group_type"`
}

// MediaView 重复组成员视图（含库路径拼接）。
type MediaView struct {
	Media
	FullPath    string `json:"full_path"`
	LibraryName string `json:"library_name"`
}

// GroupView 分组展示视图（含成员）。
type GroupView struct {
	ID        int64
	GroupType string
	Directory string
	Items     []MediaView
}

// DuplicateRepo 重复报告仓库。
type DuplicateRepo struct{ db *sql.DB }

func NewDuplicateRepo(db *sql.DB) *DuplicateRepo { return &DuplicateRepo{db: db} }

// PersistGroup 待持久化的重复组（服务层检测结果 → 存储）。
type PersistGroup struct {
	GroupType string
	MediaIDs  []int64
}

// ReplaceReport 在单事务中替换旧报告：显式清空全部旧报告数据（组/成员）→
// 写入新报告 → 更新统计。显式逐表 DELETE 不依赖外键级联，
// 避免历史外键未启用时残留孤儿组/成员。
// 组与成员使用 prepared statement 批量写入，避免逐行解析 SQL。
func (r *DuplicateRepo) ReplaceReport(rep *DuplicateReport, groups []PersistGroup) (int64, error) {
	var reportID int64
	err := WithTx(r.db, 3, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM duplicate_group_members`); err != nil {
			return errx.Wrapf(err, "清空旧重复报告成员")
		}
		if _, err := tx.Exec(`DELETE FROM duplicate_groups`); err != nil {
			return errx.Wrapf(err, "清空旧重复报告组")
		}
		if _, err := tx.Exec(`DELETE FROM duplicate_reports`); err != nil {
			return errx.Wrapf(err, "清空旧重复报告")
		}
		id, err := r.CreateReportTx(tx, rep)
		if err != nil {
			return err
		}
		reportID = id

		groupStmt, err := tx.Prepare(`INSERT INTO duplicate_groups (report_id, group_type) VALUES (?,?)`)
		if err != nil {
			return errx.Wrapf(err, "准备分组写入")
		}
		defer groupStmt.Close()
		memberStmt, err := tx.Prepare(`INSERT OR IGNORE INTO duplicate_group_members (group_id, media_id) VALUES (?,?)`)
		if err != nil {
			return errx.Wrapf(err, "准备成员写入")
		}
		defer memberStmt.Close()

		for _, pg := range groups {
			res, err := groupStmt.Exec(reportID, pg.GroupType)
			if err != nil {
				return errx.Wrapf(err, "创建重复组")
			}
			gid, _ := res.LastInsertId()
			for _, mid := range pg.MediaIDs {
				if _, err := memberStmt.Exec(gid, mid); err != nil {
					return errx.Wrapf(err, "创建重复组成员")
				}
			}
		}
		if _, err := tx.Exec(`UPDATE duplicate_reports SET
			total_groups = (SELECT COUNT(*) FROM duplicate_groups WHERE report_id = ?),
			total_files = (SELECT COUNT(*) FROM duplicate_group_members m
				JOIN duplicate_groups g ON g.id = m.group_id WHERE g.report_id = ?)
			WHERE id = ?`, reportID, reportID, reportID); err != nil {
			return errx.Wrapf(err, "更新报告统计")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return reportID, nil
}

// CreateReportTx 在事务中创建报告，返回新 ID。
func (r *DuplicateRepo) CreateReportTx(tx *sql.Tx, rep *DuplicateReport) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO duplicate_reports (background_task_id, scope, media_type, image_threshold,
			video_phash_distance, video_duration_diff_ms, oshash_filter, include_sha1)
		 VALUES (?,?,?,?,?,?,?,?)`,
		rep.BackgroundTaskID, rep.Scope, rep.MediaType, rep.ImageThreshold,
		rep.VideoPhashDistance, rep.VideoDurationDiffMs, boolToInt(rep.OshashFilter), boolToInt(rep.IncludeSHA1),
	)
	if err != nil {
		return 0, errx.Wrapf(err, "创建重复报告")
	}
	return res.LastInsertId()
}

// CreateGroupTx 在事务中创建重复组。
func (r *DuplicateRepo) CreateGroupTx(tx *sql.Tx, g *DuplicateGroup) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO duplicate_groups (report_id, group_type) VALUES (?,?)`,
		g.ReportID, g.GroupType,
	)
	if err != nil {
		return 0, errx.Wrapf(err, "创建重复组")
	}
	return res.LastInsertId()
}

// CreateMemberTx 在事务中创建组成员（唯一约束防重复）。
func (r *DuplicateRepo) CreateMemberTx(tx *sql.Tx, groupID, mediaID int64) error {
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO duplicate_group_members (group_id, media_id) VALUES (?,?)`,
		groupID, mediaID,
	); err != nil {
		return errx.Wrapf(err, "创建重复组成员")
	}
	return nil
}

// DeleteAllReports 删除全部报告（生成新报告前调用）。
// 显式逐表清空，不依赖外键级联，避免残留孤儿组/成员。
func (r *DuplicateRepo) DeleteAllReports() error {
	if _, err := r.db.Exec(`DELETE FROM duplicate_group_members`); err != nil {
		return errx.Wrapf(err, "清空重复报告成员")
	}
	if _, err := r.db.Exec(`DELETE FROM duplicate_groups`); err != nil {
		return errx.Wrapf(err, "清空重复报告组")
	}
	if _, err := r.db.Exec(`DELETE FROM duplicate_reports`); err != nil {
		return errx.Wrapf(err, "清空重复报告")
	}
	return nil
}

// GetLatestReport 返回最新一份报告；不存在时返回 (nil, nil)。
func (r *DuplicateRepo) GetLatestReport() (*DuplicateReport, error) {
	q := `SELECT id, background_task_id, scope, media_type, image_threshold,
		video_phash_distance, video_duration_diff_ms, oshash_filter, include_sha1,
		stale, total_groups, total_files, created_at
		FROM duplicate_reports ORDER BY id DESC LIMIT 1`
	rep, err := r.scanReport(q)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rep, err
}

// GetReportByID 按 ID 查询报告。
func (r *DuplicateRepo) GetReportByID(id int64) (*DuplicateReport, error) {
	return r.scanReport(
		`SELECT id, background_task_id, scope, media_type, image_threshold,
			video_phash_distance, video_duration_diff_ms, oshash_filter, include_sha1,
			stale, total_groups, total_files, created_at
			FROM duplicate_reports WHERE id = ?`, id)
}

// scanReport 扫描一行报告记录。
func (r *DuplicateRepo) scanReport(q string, args ...any) (*DuplicateReport, error) {
	var rep DuplicateReport
	var oshash, includeSha1, stale int
	err := r.db.QueryRow(q, args...).Scan(
		&rep.ID, &rep.BackgroundTaskID, &rep.Scope, &rep.MediaType, &rep.ImageThreshold,
		&rep.VideoPhashDistance, &rep.VideoDurationDiffMs, &oshash, &includeSha1,
		&stale, &rep.TotalGroups, &rep.TotalFiles, &rep.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	rep.OshashFilter = oshash == 1
	rep.IncludeSHA1 = includeSha1 == 1
	rep.Stale = stale == 1
	return &rep, nil
}

// SetStaleOnLatest 将最新报告标记为需要重新生成。
func (r *DuplicateRepo) SetStaleOnLatest() error {
	if _, err := r.db.Exec(
		`UPDATE duplicate_reports SET stale = 1
		 WHERE id = (SELECT id FROM duplicate_reports ORDER BY id DESC LIMIT 1)`,
	); err != nil {
		return errx.Wrapf(err, "标记重复报告过期")
	}
	return nil
}

// DeleteMembersByMedia 删除指定媒体在当前报告中的全部成员关系，返回删除行数。
// 用于"排除重复"：人工判定某文件无重复后将其从报告中移除（仅当前报告生效）。
func (r *DuplicateRepo) DeleteMembersByMedia(mediaID int64) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM duplicate_group_members WHERE media_id = ?`, mediaID,
	)
	if err != nil {
		return 0, errx.Wrapf(err, "删除媒体 %d 的重复组成员关系", mediaID)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// PruneGroupsAndUpdateStats 清理成员数 <2 的重复组，并刷新最新报告统计。
func (r *DuplicateRepo) PruneGroupsAndUpdateStats() error {
	if _, err := r.db.Exec(
		`DELETE FROM duplicate_groups WHERE id NOT IN (
			SELECT group_id FROM duplicate_group_members GROUP BY group_id HAVING COUNT(*) >= 2
		)`,
	); err != nil {
		return errx.Wrapf(err, "清理不足 2 个成员的重复组")
	}
	if _, err := r.db.Exec(
		`UPDATE duplicate_reports SET
			total_groups = (SELECT COUNT(*) FROM duplicate_groups WHERE report_id = duplicate_reports.id),
			total_files = (SELECT COUNT(*) FROM duplicate_group_members m
				JOIN duplicate_groups g ON g.id = m.group_id
				WHERE g.report_id = duplicate_reports.id)
		 WHERE id = (SELECT id FROM duplicate_reports ORDER BY id DESC LIMIT 1)`,
	); err != nil {
		return errx.Wrapf(err, "刷新重复报告统计")
	}
	return nil
}

// GroupViews 加载报告的全部分组（含成员与完整路径），供服务层分页/树视图使用。
func (r *DuplicateRepo) GroupViews(reportID int64) ([]GroupView, error) {
	rows, err := r.db.Query(
		`SELECT g.id, g.group_type,
			m.id, m.library_id, m.scan_session_id, m.kind, m.relative_path, m.file_size, m.mtime,
			m.format, m.width, m.height, m.phash, m.dhash, m.ahash, m.duration_ms,
			m.video_codec, m.audio_codec, m.frame_rate, m.bit_rate, m.oshash, m.sha1,
			m.thumbnail_path, m.created_at,
			l.path, l.name
		 FROM duplicate_groups g
		 JOIN duplicate_group_members dgm ON dgm.group_id = g.id
		 JOIN media m ON m.id = dgm.media_id
		 JOIN libraries l ON l.id = m.library_id
		 WHERE g.report_id = ?
		 ORDER BY g.id, m.relative_path`, reportID,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "加载重复分组")
	}
	defer rows.Close()

	groupIndex := map[int64]int{}
	views := make([]GroupView, 0)
	for rows.Next() {
		var gid int64
		var gtype string
		var mv MediaView
		var libPath, libName string
		if err := rows.Scan(&gid, &gtype,
			&mv.ID, &mv.LibraryID, &mv.ScanSessionID, &mv.Kind, &mv.RelativePath, &mv.FileSize, &mv.Mtime,
			&mv.Format, &mv.Width, &mv.Height, &mv.Phash, &mv.Dhash, &mv.Ahash, &mv.DurationMs,
			&mv.VideoCodec, &mv.AudioCodec, &mv.FrameRate, &mv.BitRate, &mv.Oshash, &mv.Sha1,
			&mv.ThumbnailPath, &mv.CreatedAt,
			&libPath, &libName); err != nil {
			return nil, errx.Wrapf(err, "扫描重复分组行")
		}
		mv.FullPath = joinPath(libPath, mv.RelativePath)
		mv.LibraryName = libName

		idx, ok := groupIndex[gid]
		if !ok {
			views = append(views, GroupView{ID: gid, GroupType: gtype, Items: make([]MediaView, 0, 2)})
			groupIndex[gid] = len(views) - 1
			idx = len(views) - 1
		}
		views[idx].Items = append(views[idx].Items, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, errx.Wrapf(err, "遍历重复分组")
	}
	return views, nil
}

// JoinPath 拼接库路径与相对路径（统一正斜杠展示）。
func joinPath(libPath, relPath string) string {
	if libPath == "" {
		return relPath
	}
	return strings.TrimRight(libPath, `/\`) + "/" + relPath
}
