// 包 db：SQLite 连接与内嵌 schema 迁移。
// 代码注释使用中文。
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	"memable/internal/config"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// schemaVersion 当前迁移版本；schema.sql 每次变更须 +1 并在 Migrate 中追加对应迁移。
const schemaVersion = 2

// Open 建立带 WAL/foreign_keys/busy_timeout 的 SQLite 连接。
func Open(cfg *config.Config) (*sql.DB, error) {
	// 参数化 DSN，避免直接拼 PRAGMA；_time_format=sqlite 使 TIMESTAMP 列自动解析为 time.Time
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_time_format=sqlite", cfg.Database.Path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开连接: %w", err)
	}
	// SQLite 单写者；连接池上限 1 可避免 SQLITE_BUSY 概率
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}

// Migrate 读取 schema_version，按需执行内嵌 schema.sql。
// 简化策略：版本 1 直接执行整个 schema.sql；未来新增表/字段时，追加独立 ALTER 迁移。
func Migrate(db *sql.DB) error {
	var cur int
	row := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`)
	if err := row.Scan(&cur); err != nil {
		// 表不存在时也返回错误，先尝试执行 schema 再读
		cur = 0
	}
	if cur >= schemaVersion {
		return nil
	}

	if cur == 0 {
		if _, err := db.Exec(schemaSQL); err != nil {
			return fmt.Errorf("执行内嵌 schema.sql: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("写入 schema_version: %w", err)
		}
		return nil
	}

	// v2：术语统一为 oshash，并移除已废弃的视频关键帧持久化表。
	if cur < 2 {
		if _, err := db.Exec(`ALTER TABLE media RENAME COLUMN ohash TO oshash;
			DROP TABLE IF EXISTS video_frames;
			CREATE INDEX IF NOT EXISTS idx_media_oshash ON media(oshash) WHERE oshash IS NOT NULL;
			INSERT INTO schema_version(version) VALUES (2);`); err != nil {
			return fmt.Errorf("迁移到 v2: %w", err)
		}
	}
	return nil
}

// SchemaVersion 返回当前数据库版本，供调试/healthcheck 使用。
func SchemaVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&v)
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return 0, nil
	}
	return v, err
}
