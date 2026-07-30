// file_stats_repo.go：file_stats 表 CRUD。
// 代码注释使用中文
package repo

import (
	"database/sql"
	"memable/internal/errx"
)

// FileStatsRepo file_stats 表仓库。
type FileStatsRepo struct{ db *sql.DB }

func NewFileStatsRepo(db *sql.DB) *FileStatsRepo { return &FileStatsRepo{db: db} }

const fileStatsCols = `id, dir_path, total_bytes, total_count, ext_stats, file_tree, created_at`

// Create 新建文件统计记录。
func (r *FileStatsRepo) Create(fs *FileStats) error {
	res, err := r.db.Exec(
		`INSERT INTO file_stats (dir_path, total_bytes, total_count, ext_stats, file_tree)
		 VALUES (?, ?, ?, ?, ?)`,
		fs.DirPath, fs.TotalBytes, fs.TotalCount, fs.ExtStats, fs.FileTree,
	)
	if err != nil {
		return errx.Wrapf(err, "创建文件统计记录 %q", fs.DirPath)
	}
	fs.ID, _ = res.LastInsertId()
	return nil
}

// GetByID 按主键查询单条记录。
func (r *FileStatsRepo) GetByID(id int64) (*FileStats, error) {
	var fs FileStats
	err := r.db.QueryRow(
		`SELECT `+fileStatsCols+` FROM file_stats WHERE id = ?`, id,
	).Scan(&fs.ID, &fs.DirPath, &fs.TotalBytes, &fs.TotalCount,
		&fs.ExtStats, &fs.FileTree, &fs.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, errx.Newf("文件统计记录 id=%d 不存在", id)
	}
	if err != nil {
		return nil, errx.Wrapf(err, "查询文件统计记录 id=%d", id)
	}
	return &fs, nil
}

// List 查询所有统计记录，按创建时间倒序。
func (r *FileStatsRepo) List(limit, offset int) ([]FileStats, error) {
	q := `SELECT ` + fileStatsCols + ` FROM file_stats ORDER BY created_at DESC`
	if limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		return r.query(q, limit, offset)
	}
	return r.query(q)
}

// Delete 删除统计记录。
func (r *FileStatsRepo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM file_stats WHERE id = ?`, id)
	if err != nil {
		return errx.Wrapf(err, "删除文件统计记录 id=%d", id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("文件统计记录 id=%d 不存在", id)
	}
	return nil
}

// query 通用多行查询。
func (r *FileStatsRepo) query(q string, args ...any) ([]FileStats, error) {
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, errx.Wrapf(err, "文件统计查询")
	}
	defer rows.Close()

	out := make([]FileStats, 0)
	for rows.Next() {
		var fs FileStats
		if err := rows.Scan(&fs.ID, &fs.DirPath, &fs.TotalBytes, &fs.TotalCount,
			&fs.ExtStats, &fs.FileTree, &fs.CreatedAt); err != nil {
			return nil, errx.Wrapf(err, "扫描文件统计行")
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}
