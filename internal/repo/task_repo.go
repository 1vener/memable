// task_repo.go：background_tasks 表 CRUD。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"time"

	"memable/internal/errx"
)

// TaskRepo 后台任务仓库。
type TaskRepo struct{ db *sql.DB }

func NewTaskRepo(db *sql.DB) *TaskRepo { return &TaskRepo{db: db} }

const taskCols = `id, kind, status, title, dedupe_key, library_id, scan_session_id,
	payload_json, phase, total_items, processed_items, succeeded_items, skipped_items,
	failed_items, result_json, error_message, queued_at, started_at, updated_at, finished_at`

// Create 创建任务；dedupe_key 非空时自动去重检查。
func (r *TaskRepo) Create(t *BackgroundTask) error {
	res, err := r.db.Exec(
		`INSERT INTO background_tasks (id, kind, status, title, dedupe_key, library_id,
			scan_session_id, payload_json, phase)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Kind, t.Status, t.Title, t.DedupeKey, t.LibraryID,
		t.ScanSessionID, t.PayloadJSON, t.Phase,
	)
	if err != nil {
		return errx.Wrapf(err, "创建后台任务 %s", t.ID)
	}
	if id, _ := res.LastInsertId(); id != 0 {
		// background_tasks 使用 TEXT 主键，忽略
	}
	return nil
}

// GetByID 按 ID 查询。
func (r *TaskRepo) GetByID(id string) (*BackgroundTask, error) {
	var t BackgroundTask
	err := r.db.QueryRow(
		`SELECT `+taskCols+` FROM background_tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Kind, &t.Status, &t.Title, &t.DedupeKey, &t.LibraryID,
		&t.ScanSessionID, &t.PayloadJSON, &t.Phase, &t.TotalItems, &t.ProcessedItems,
		&t.SucceededItems, &t.SkippedItems, &t.FailedItems, &t.ResultJSON, &t.ErrorMessage,
		&t.QueuedAt, &t.StartedAt, &t.UpdatedAt, &t.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, errx.Newf("任务 %s 不存在", id)
	}
	if err != nil {
		return nil, errx.Wrapf(err, "查询任务 %s", id)
	}
	return &t, nil
}

// FindActiveByDedupe 查找与去重键匹配的 queued/running 任务。
func (r *TaskRepo) FindActiveByDedupe(dedupeKey string) (*BackgroundTask, error) {
	var t BackgroundTask
	err := r.db.QueryRow(
		`SELECT `+taskCols+` FROM background_tasks
		 WHERE dedupe_key = ? AND status IN ('queued','running') LIMIT 1`, dedupeKey,
	).Scan(&t.ID, &t.Kind, &t.Status, &t.Title, &t.DedupeKey, &t.LibraryID,
		&t.ScanSessionID, &t.PayloadJSON, &t.Phase, &t.TotalItems, &t.ProcessedItems,
		&t.SucceededItems, &t.SkippedItems, &t.FailedItems, &t.ResultJSON, &t.ErrorMessage,
		&t.QueuedAt, &t.StartedAt, &t.UpdatedAt, &t.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.Wrapf(err, "查找重复任务 %q", dedupeKey)
	}
	return &t, nil
}

// ListByStatus 按状态查询任务，按创建时间升序。
func (r *TaskRepo) ListByStatus(status string, limit, offset int) ([]BackgroundTask, error) {
	q := `SELECT ` + taskCols + ` FROM background_tasks WHERE status = ? ORDER BY queued_at ASC`
	if limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		return r.query(q, status, limit, offset)
	}
	return r.query(q, status)
}

// ListAll 查询所有任务，queued/running 优先，再按创建时间倒序。
func (r *TaskRepo) ListAll(limit, offset int) ([]BackgroundTask, error) {
	q := `SELECT ` + taskCols + ` FROM background_tasks
		ORDER BY CASE status WHEN 'queued' THEN 0 WHEN 'running' THEN 1 ELSE 2 END, queued_at DESC`
	if limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		return r.query(q, limit, offset)
	}
	return r.query(q)
}

// DequeueNext 取出队列中最早的 queued 任务并置为 running。
func (r *TaskRepo) DequeueNext() (*BackgroundTask, error) {
	var t BackgroundTask
	err := r.db.QueryRow(
		`SELECT `+taskCols+` FROM background_tasks
		 WHERE status = 'queued' ORDER BY queued_at ASC LIMIT 1`,
	).Scan(&t.ID, &t.Kind, &t.Status, &t.Title, &t.DedupeKey, &t.LibraryID,
		&t.ScanSessionID, &t.PayloadJSON, &t.Phase, &t.TotalItems, &t.ProcessedItems,
		&t.SucceededItems, &t.SkippedItems, &t.FailedItems, &t.ResultJSON, &t.ErrorMessage,
		&t.QueuedAt, &t.StartedAt, &t.UpdatedAt, &t.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.Wrapf(err, "取出下一任务")
	}
	now := time.Now().UTC()
	res, err := r.db.Exec(
		`UPDATE background_tasks SET status = 'running', started_at = ?, updated_at = ?
		 WHERE id = ? AND status = 'queued'`, now, now, t.ID,
	)
	if err != nil {
		return nil, errx.Wrapf(err, "启动任务 %s", t.ID)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil // 被并发抢占
	}
	t.Status = TaskStatusRunning
	t.StartedAt = &now
	return &t, nil
}

// UpdateProgress 更新进度。
func (r *TaskRepo) UpdateProgress(id string, phase string, total, processed, succeeded, skipped, failed int) error {
	_, err := r.db.Exec(
		`UPDATE background_tasks SET phase = ?, total_items = ?, processed_items = ?,
		 succeeded_items = ?, skipped_items = ?, failed_items = ?, updated_at = datetime('now')
		 WHERE id = ?`, phase, total, processed, succeeded, skipped, failed, id,
	)
	return errx.Wrapf(err, "更新任务 %s 进度", id)
}

// Complete 标记任务完成。
func (r *TaskRepo) Complete(id string, resultJSON string) error {
	_, err := r.db.Exec(
		`UPDATE background_tasks SET status = 'completed', result_json = ?,
		 finished_at = datetime('now'), updated_at = datetime('now')
		 WHERE id = ?`, resultJSON, id,
	)
	return errx.Wrapf(err, "完成任务 %s", id)
}

// Fail 标记任务失败。
func (r *TaskRepo) Fail(id string, errMsg string) error {
	_, err := r.db.Exec(
		`UPDATE background_tasks SET status = 'failed', error_message = ?,
		 finished_at = datetime('now'), updated_at = datetime('now')
		 WHERE id = ?`, errMsg, id,
	)
	return errx.Wrapf(err, "失败任务 %s", id)
}

// Cancel 取消任务。
func (r *TaskRepo) Cancel(id string) error {
	res, err := r.db.Exec(
		`UPDATE background_tasks SET status = 'cancelled',
		 finished_at = datetime('now'), updated_at = datetime('now')
		 WHERE id = ? AND status IN ('queued','running')`, id,
	)
	if err != nil {
		return errx.Wrapf(err, "取消任务 %s", id)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errx.Newf("任务 %s 不存在或已终态", id)
	}
	return nil
}

// ResetRunning 将遗留的 running 任务标记为 failed（服务重启恢复）。
func (r *TaskRepo) ResetRunning() (int, error) {
	res, err := r.db.Exec(
		`UPDATE background_tasks SET status = 'failed',
		 error_message = '服务重启导致任务中断',
		 finished_at = datetime('now'), updated_at = datetime('now')
		 WHERE status = 'running'`,
	)
	if err != nil {
		return 0, errx.Wrapf(err, "重置遗留运行任务")
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// HasActiveForLibrary 检查指定库是否有排队中或运行中的任务。
func (r *TaskRepo) HasActiveForLibrary(libraryID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM background_tasks
		 WHERE library_id = ? AND status IN ('queued','running')`, libraryID,
	).Scan(&n)
	return n > 0, errx.Wrapf(err, "检查库 %d 是否有活动任务", libraryID)
}

// HasActiveForSession 检查指定扫描会话是否有排队中或运行中的任务。
func (r *TaskRepo) HasActiveForSession(sessionID string) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM background_tasks
		 WHERE scan_session_id = ? AND status IN ('queued','running')`, sessionID,
	).Scan(&n)
	return n > 0, errx.Wrapf(err, "检查会话 %s 是否有活动任务", sessionID)
}

// QueuePosition 获取指定任务的排队位置（仅 queued 任务有效）。
func (r *TaskRepo) QueuePosition(id string) (int, error) {
	var pos int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM background_tasks
		 WHERE status = 'queued' AND queued_at < (
			SELECT queued_at FROM background_tasks WHERE id = ?
		 )`, id,
	).Scan(&pos)
	return pos + 1, errx.Wrapf(err, "计算任务 %s 排队位置", id)
}

// query 通用多行查询。
func (r *TaskRepo) query(q string, args ...any) ([]BackgroundTask, error) {
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, errx.Wrapf(err, "任务查询")
	}
	defer rows.Close()

	out := make([]BackgroundTask, 0)
	for rows.Next() {
		var t BackgroundTask
		if err := rows.Scan(&t.ID, &t.Kind, &t.Status, &t.Title, &t.DedupeKey, &t.LibraryID,
			&t.ScanSessionID, &t.PayloadJSON, &t.Phase, &t.TotalItems, &t.ProcessedItems,
			&t.SucceededItems, &t.SkippedItems, &t.FailedItems, &t.ResultJSON, &t.ErrorMessage,
			&t.QueuedAt, &t.StartedAt, &t.UpdatedAt, &t.FinishedAt); err != nil {
			return nil, errx.Wrapf(err, "扫描任务行")
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
