// 包 db：SQLite 连接与 v1 数据库初始化。
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

// schemaVersion 当前数据库结构版本。
const schemaVersion = 1

// Open 建立带 WAL/foreign_keys/busy_timeout 的 SQLite 连接。
func Open(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_time_format=sqlite", cfg.Database.Path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开连接: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}

// Migrate 仅负责首次建库，不执行版本升级或历史兼容迁移。
// SQLite 在 Open 时会创建空文件，因此以 schema_version 表是否存在判断数据库是否已初始化。
func Migrate(db *sql.DB) error {
	var initialized int
	if err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_version'
	)`).Scan(&initialized); err != nil {
		return fmt.Errorf("检查数据库是否已初始化: %w", err)
	}
	if initialized != 0 {
		return nil
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("执行数据库初始化脚本: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES (?)`, schemaVersion); err != nil {
		return fmt.Errorf("写入 schema_version: %w", err)
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
