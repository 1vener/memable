// library_repo.go：libraries 表 CRUD。
// 代码注释使用中文。
package repo

import (
	"database/sql"

	"memable/internal/errx"
)

// LibraryRepo libraries 表仓库。
type LibraryRepo struct{ db *sql.DB }

func NewLibraryRepo(db *sql.DB) *LibraryRepo { return &LibraryRepo{db: db} }

// Create 新建收藏库；name 唯一。
func (r *LibraryRepo) Create(l *Library) error {
	res, err := r.db.Exec(
		`INSERT INTO libraries (name, path, kind) VALUES (?, ?, ?)`,
		l.Name, l.Path, l.Kind,
	)
	if err != nil {
		return errx.Wrapf(err, "创建收藏库 %q", l.Name)
	}
	l.ID, _ = res.LastInsertId()
	return nil
}

// GetByID 按主键查询。
func (r *LibraryRepo) GetByID(id int64) (*Library, error) {
	var l Library
	err := r.db.QueryRow(
		`SELECT id, name, path, kind, created_at, updated_at FROM libraries WHERE id = ?`, id,
	).Scan(&l.ID, &l.Name, &l.Path, &l.Kind, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, errx.Wrapf(err, "查询收藏库 id=%d", id)
	}
	return &l, nil
}

// List 查询全部收藏库。
func (r *LibraryRepo) List() ([]Library, error) {
	rows, err := r.db.Query(`SELECT id, name, path, kind, created_at, updated_at FROM libraries ORDER BY id`)
	if err != nil {
		return nil, errx.Wrapf(err, "列出收藏库")
	}
	defer rows.Close()

	var out []Library
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.Name, &l.Path, &l.Kind, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, errx.Wrapf(err, "扫描收藏库行")
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// UpdatePath 仅更新根目录路径（库迁移），相对路径不变，无需重扫。
func (r *LibraryRepo) UpdatePath(id int64, newPath string) error {
	res, err := r.db.Exec(
		`UPDATE libraries SET path = ?, updated_at = datetime('now') WHERE id = ?`, newPath, id,
	)
	if err != nil {
		return errx.Wrapf(err, "迁移收藏库 id=%d 到 %q", id, newPath)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("收藏库 id=%d 不存在", id)
	}
	return nil
}

// Delete 删除收藏库；media 表外键级联删除。
func (r *LibraryRepo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return errx.Wrapf(err, "删除收藏库 id=%d", id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("收藏库 id=%d 不存在", id)
	}
	return nil
}
