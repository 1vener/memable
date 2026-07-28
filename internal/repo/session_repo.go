// session_repo.go：scan_sessions 表 CRUD。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"time"

	"memable/internal/errx"
)

// SessionRepo scan_sessions 表仓库。
type SessionRepo struct{ db *sql.DB }

func NewSessionRepo(db *sql.DB) *SessionRepo { return &SessionRepo{db: db} }

// Create 新建扫描会话；ID 由调用方生成（UUID）。
func (r *SessionRepo) Create(s *ScanSession) error {
	_, err := r.db.Exec(
		`INSERT INTO scan_sessions (id, library_id, is_temporary, status) VALUES (?, ?, ?, ?)`,
		s.ID, s.LibraryID, boolToInt(s.IsTemporary), s.Status,
	)
	return errx.Wrapf(err, "创建扫描会话 %s", s.ID)
}

// UpdateStatus 更新会话状态；终态同时写入 finished_at。
func (r *SessionRepo) UpdateStatus(id, status string) error {
	var finished *time.Time
	if status == "completed" || status == "failed" || status == "cancelled" || status == "promoted" {
		now := time.Now().UTC()
		finished = &now
	}
	res, err := r.db.Exec(
		`UPDATE scan_sessions SET status = ?, finished_at = COALESCE(?, finished_at) WHERE id = ?`,
		status, finished, id,
	)
	if err != nil {
		return errx.Wrapf(err, "更新会话 %s 状态为 %s", id, status)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("扫描会话 %s 不存在", id)
	}
	return nil
}

// GetByID 按 ID 查询会话。
func (r *SessionRepo) GetByID(id string) (*ScanSession, error) {
	var s ScanSession
	var tmp int
	err := r.db.QueryRow(
		`SELECT id, library_id, is_temporary, status, started_at, finished_at FROM scan_sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.LibraryID, &tmp, &s.Status, &s.StartedAt, &s.FinishedAt)
	if err != nil {
		return nil, errx.Wrapf(err, "查询扫描会话 %s", id)
	}
	s.IsTemporary = tmp == 1
	return &s, nil
}

// Promote 将会话标记为已入库（is_temporary=0, status=promoted）。
func (r *SessionRepo) Promote(id string) error {
	res, err := r.db.Exec(
		`UPDATE scan_sessions SET is_temporary = 0, status = 'promoted',
		 finished_at = COALESCE(finished_at, datetime('now')) WHERE id = ?`, id,
	)
	if err != nil {
		return errx.Wrapf(err, "会话 %s 入库", id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("扫描会话 %s 不存在", id)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
