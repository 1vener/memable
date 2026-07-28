// tx.go：事务封装与 SQLITE_BUSY 重试。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"memable/internal/errx"
)

// TxFunc 事务内执行的函数。
type TxFunc func(tx *sql.Tx) error

// WithTx 在事务中执行 fn；出错回滚，提交时遇到 SQLITE_BUSY 自动重试。
// maxRetries 建议 3~5；SQLite 单写者模型下 busy_timeout 已覆盖大部分场景，
// 此处的重试用于兜底 WAL 模式下的并发提交冲突。
func WithTx(db *sql.DB, maxRetries int, fn TxFunc) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		err = withTxOnce(db, fn)
		if err == nil || !isBusy(err) {
			return err
		}
		// 指数退避：50ms, 100ms, 200ms ...
		time.Sleep(time.Duration(50<<i) * time.Millisecond)
	}
	return errx.Wrapf(err, "事务重试 %d 次后仍失败", maxRetries)
}

func withTxOnce(db *sql.DB, fn TxFunc) error {
	tx, err := db.Begin()
	if err != nil {
		return errx.Wrapf(err, "开启事务")
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return errx.Wrapf(err, "提交事务")
	}
	return nil
}

// isBusy 判断是否为 SQLITE_BUSY 类错误（modernc.org/sqlite 错误信息包含 5/SQLITE_BUSY）。
func isBusy(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrTxDone) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}
