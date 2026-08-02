// dir_duplicate_repo.go：目录对比报告三张表（dir_duplicate_reports/groups/members）的仓库。
// 目录对比 = 所选目录（含子目录）与其余存量数据的重复检测，独立存储，不替换重复报告。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"time"

	"memable/internal/errx"
)

// DirDuplicateReport 目录对比报告主表记录。
type DirDuplicateReport struct {
	ID                  int64     `json:"id"`
	BackgroundTaskID    *string   `json:"background_task_id,omitempty"`
	LibraryID           int64     `json:"library_id"`
	Directory           string    `json:"directory"` // 所选目录相对库根路径（正斜杠，含子目录）
	MediaType           string    `json:"media_type"`
	ImageThreshold      int       `json:"image_threshold"`
	VideoPhashDistance  int       `json:"video_phash_distance"`
	VideoDurationDiffMs int64     `json:"video_duration_diff_ms"`
	OshashFilter        bool      `json:"oshash_filter"`
	IncludeSHA1         bool      `json:"include_sha1"`
	TotalGroups         int       `json:"total_groups"`
	TotalFiles          int       `json:"total_files"`
	CreatedAt           time.Time `json:"created_at"`
}

// DirGroupView 目录对比分组视图（含成员与完整路径）。
type DirGroupView struct {
	ID        int64
	GroupType string
	Items     []DirMemberView
}

// DirMemberView 目录对比组成员视图。
type DirMemberView struct {
	MediaView
	IsTarget bool `json:"is_target"` // true=所选目录文件
}

// DirPersistGroup 待持久化的目录对比分组。
type DirPersistGroup struct {
	GroupType string
	Members   []DirMemberPersist // 含目标标记
}

// DirMemberPersist 待持久化的成员（media_id + 目标标记）。
type DirMemberPersist struct {
	MediaID  int64
	IsTarget bool
}

// DirDuplicateRepo 目录对比报告仓库。
type DirDuplicateRepo struct{ db *sql.DB }

func NewDirDuplicateRepo(db *sql.DB) *DirDuplicateRepo { return &DirDuplicateRepo{db: db} }

// ReplaceDirReport 在单事务中替换旧目录对比报告：显式逐表清空 → 写入新报告 → 更新统计。
func (r *DirDuplicateRepo) ReplaceDirReport(rep *DirDuplicateReport, groups []DirPersistGroup) (int64, error) {
	var reportID int64
	err := WithTx(r.db, 3, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM dir_duplicate_members`); err != nil {
			return errx.Wrapf(err, "清空旧目录对比成员")
		}
		if _, err := tx.Exec(`DELETE FROM dir_duplicate_groups`); err != nil {
			return errx.Wrapf(err, "清空旧目录对比组")
		}
		if _, err := tx.Exec(`DELETE FROM dir_duplicate_reports`); err != nil {
			return errx.Wrapf(err, "清空旧目录对比报告")
		}

		res, err := tx.Exec(
			`INSERT INTO dir_duplicate_reports (background_task_id, library_id, directory,
				media_type, image_threshold, video_phash_distance, video_duration_diff_ms,
				oshash_filter, include_sha1)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			rep.BackgroundTaskID, rep.LibraryID, rep.Directory,
			rep.MediaType, rep.ImageThreshold, rep.VideoPhashDistance, rep.VideoDurationDiffMs,
			boolToInt(rep.OshashFilter), boolToInt(rep.IncludeSHA1),
		)
		if err != nil {
			return errx.Wrapf(err, "创建目录对比报告")
		}
		reportID, _ = res.LastInsertId()

		groupStmt, err := tx.Prepare(`INSERT INTO dir_duplicate_groups (report_id, group_type) VALUES (?,?)`)
		if err != nil {
			return errx.Wrapf(err, "准备目录对比组写入")
		}
		defer groupStmt.Close()
		memberStmt, err := tx.Prepare(`INSERT OR IGNORE INTO dir_duplicate_members (group_id, media_id, is_target) VALUES (?,?,?)`)
		if err != nil {
			return errx.Wrapf(err, "准备目录对比成员写入")
		}
		defer memberStmt.Close()

		for _, pg := range groups {
			gid, err := func() (int64, error) {
				res, err := groupStmt.Exec(reportID, pg.GroupType)
				if err != nil {
					return 0, errx.Wrapf(err, "创建目录对比组")
				}
				return res.LastInsertId()
			}()
			if err != nil {
				return err
			}
			for _, mem := range pg.Members {
				if _, err := memberStmt.Exec(gid, mem.MediaID, boolToInt(mem.IsTarget)); err != nil {
					return errx.Wrapf(err, "创建目录对比成员")
				}
			}
		}

		if _, err := tx.Exec(`UPDATE dir_duplicate_reports SET
			total_groups = (SELECT COUNT(*) FROM dir_duplicate_groups WHERE report_id = ?),
			total_files = (SELECT COUNT(*) FROM dir_duplicate_members m
				JOIN dir_duplicate_groups g ON g.id = m.group_id WHERE g.report_id = ?)
			WHERE id = ?`, reportID, reportID, reportID); err != nil {
			return errx.Wrapf(err, "更新目录对比报告统计")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return reportID, nil
}

// GetLatestDirReport 返回最新一份目录对比报告；不存在时返回 (nil, nil)。
func (r *DirDuplicateRepo) GetLatestDirReport() (*DirDuplicateReport, error) {
	var rep DirDuplicateReport
	var oshash, includeSha1 int
	err := r.db.QueryRow(
		`SELECT id, background_task_id, library_id, directory, media_type, image_threshold,
			video_phash_distance, video_duration_diff_ms, oshash_filter, include_sha1,
			total_groups, total_files, created_at
		 FROM dir_duplicate_reports ORDER BY id DESC LIMIT 1`,
	).Scan(&rep.ID, &rep.BackgroundTaskID, &rep.LibraryID, &rep.Directory, &rep.MediaType,
		&rep.ImageThreshold, &rep.VideoPhashDistance, &rep.VideoDurationDiffMs,
		&oshash, &includeSha1, &rep.TotalGroups, &rep.TotalFiles, &rep.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.Wrapf(err, "查询最新目录对比报告")
	}
	rep.OshashFilter = oshash == 1
	rep.IncludeSHA1 = includeSha1 == 1
	return &rep, nil
}

// DirGroupViews 加载目录对比报告的全部分组（含成员、完整路径与目标标记）。
func (r *DirDuplicateRepo) DirGroupViews(reportID int64) ([]DirGroupView, error) {
	rows, err := r.db.Query(
		`SELECT g.id, g.group_type, dgm.is_target,
			m.id, m.library_id, m.scan_session_id, m.kind, m.relative_path, m.file_size, m.mtime,
			m.format, m.width, m.height, m.phash, m.dhash, m.ahash, m.duration_ms,
			m.video_codec, m.audio_codec, m.frame_rate, m.bit_rate, m.oshash, m.sha1,
			m.thumbnail_path, m.created_at,
			l.path, l.name
		 FROM dir_duplicate_groups g
		 JOIN dir_duplicate_members dgm ON dgm.group_id = g.id
		 JOIN media m ON m.id = dgm.media_id
		 JOIN libraries l ON l.id = m.library_id
		 WHERE g.report_id = ?
		 ORDER BY g.id, m.relative_path`, reportID,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "加载目录对比分组")
	}
	defer rows.Close()

	groupIndex := map[int64]int{}
	views := make([]DirGroupView, 0)
	for rows.Next() {
		var gid int64
		var gtype string
		var isTarget int
		var mv MediaView
		var libPath, libName string
		if err := rows.Scan(&gid, &gtype, &isTarget,
			&mv.ID, &mv.LibraryID, &mv.ScanSessionID, &mv.Kind, &mv.RelativePath, &mv.FileSize, &mv.Mtime,
			&mv.Format, &mv.Width, &mv.Height, &mv.Phash, &mv.Dhash, &mv.Ahash, &mv.DurationMs,
			&mv.VideoCodec, &mv.AudioCodec, &mv.FrameRate, &mv.BitRate, &mv.Oshash, &mv.Sha1,
			&mv.ThumbnailPath, &mv.CreatedAt,
			&libPath, &libName); err != nil {
			return nil, errx.Wrapf(err, "扫描目录对比分组行")
		}
		mv.FullPath = joinPath(libPath, mv.RelativePath)
		mv.LibraryName = libName

		idx, ok := groupIndex[gid]
		if !ok {
			views = append(views, DirGroupView{ID: gid, GroupType: gtype, Items: make([]DirMemberView, 0, 2)})
			groupIndex[gid] = len(views) - 1
			idx = len(views) - 1
		}
		views[idx].Items = append(views[idx].Items, DirMemberView{MediaView: mv, IsTarget: isTarget == 1})
	}
	if err := rows.Err(); err != nil {
		return nil, errx.Wrapf(err, "遍历目录对比分组")
	}
	return views, nil
}
