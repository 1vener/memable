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

//go:embed migrations/002_duplicate_report.sql
var migrationV2SQL string

//go:embed migrations/003_report_task_kind.sql
var migrationV3SQL string

//go:embed migrations/004_scan_sha1_task_kind.sql
var migrationV4SQL string

//go:embed migrations/005_media_indexes.sql
var migrationV5SQL string

//go:embed migrations/006_directory_compare.sql
var migrationV6SQL string

// schemaVersion 当前数据库结构版本。
const schemaVersion = 6

// Open 建立带 WAL/foreign_keys/busy_timeout 的 SQLite 连接。
// synchronous=NORMAL 在 WAL 模式下兼顾安全与写入性能；cache_size/mmap_size 提高大批量扫描时的读缓存。
// 注意：modernc 驱动只认 _pragma 参数（_journal_mode/_foreign_keys 等裸参数会被静默忽略）。
func Open(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_time_format=sqlite"+
		"&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"+
		"&_pragma=synchronous(NORMAL)&_pragma=cache_size(-20000)&_pragma=mmap_size(268435456)", cfg.Database.Path)
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

// Migrate 首次建库 + 增量版本迁移。
func Migrate(db *sql.DB) error {
	var initialized int
	if err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_version'
	)`).Scan(&initialized); err != nil {
		return fmt.Errorf("检查数据库是否已初始化: %w", err)
	}
	if initialized == 0 {
		if _, err := db.Exec(schemaSQL); err != nil {
			return fmt.Errorf("执行数据库初始化脚本: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES (?)`, 1); err != nil {
			return fmt.Errorf("写入 schema_version: %w", err)
		}
	}

	// 增量迁移步骤
	steps := []struct {
		version int
		sql     string
	}{
		{version: 2, sql: migrationV2SQL},
		{version: 3, sql: migrationV3SQL},
		{version: 4, sql: migrationV4SQL},
		{version: 5, sql: migrationV5SQL},
		{version: 6, sql: migrationV6SQL},
	}
	cur, err := SchemaVersion(db)
	if err != nil {
		return fmt.Errorf("读取数据库版本: %w", err)
	}
	for _, st := range steps {
		if cur >= st.version {
			continue
		}
		if _, err := db.Exec(st.sql); err != nil {
			return fmt.Errorf("执行迁移 v%d: %w", st.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version(version) VALUES (?)`, st.version); err != nil {
			return fmt.Errorf("写入 schema_version v%d: %w", st.version, err)
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
