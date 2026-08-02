// 包 db 测试：内存库上验证 Open + Migrate。
// 代码注释使用中文。
package db

import (
	"path/filepath"
	"testing"

	"memable/internal/config"
)

func TestOpenAndMigrateInMemory(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: ":memory:"},
	}
	dbh, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbh.Close()

	if err := Migrate(dbh); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	v, err := SchemaVersion(dbh)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != schemaVersion {
		t.Fatalf("expected version %d, got %d", schemaVersion, v)
	}

	// 验证核心表与重复报告三张表已建立
	for _, tbl := range []string{
		"libraries", "scan_sessions", "media", "file_stats",
		"duplicate_reports", "duplicate_groups", "duplicate_group_members",
	} {
		var n int
		q := "SELECT count(*) FROM " + tbl
		if err := dbh.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("表 %s 不存在或查询失败: %v", tbl, err)
		}
	}
}

func TestMigrateExistingDatabaseDoesNothing(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbh.Close()

	if err := Migrate(dbh); err != nil {
		t.Fatalf("首次 Migrate: %v", err)
	}
	if _, err := dbh.Exec(`CREATE TABLE migrate_sentinel (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("创建哨兵表: %v", err)
	}
	if _, err := dbh.Exec(`UPDATE schema_version SET version=99 WHERE version=1`); err != nil {
		t.Fatalf("修改版本记录: %v", err)
	}

	if err := Migrate(dbh); err != nil {
		t.Fatalf("已有数据库不应执行迁移: %v", err)
	}
	var version int
	if err := dbh.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("读取版本: %v", err)
	}
	if version != 99 {
		t.Fatalf("已有数据库被修改，version=%d", version)
	}
}

func TestMigrateV4AcceptsScanSha1Kind(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbh.Close()

	if err := Migrate(dbh); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := dbh.Exec(
		`INSERT INTO background_tasks (id, kind, title) VALUES ('t-sha1', 'scan_sha1', '补齐 SHA1')`,
	); err != nil {
		t.Fatalf("scan_sha1 任务类型被 CHECK 约束拒绝: %v", err)
	}
}

func TestMigrateV7AddsCoverPHashColumn(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbh.Close()

	if err := Migrate(dbh); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// media.cover_phash 列必须存在（v7 迁移）
	var n int
	if err := dbh.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('media') WHERE name='cover_phash'`,
	).Scan(&n); err != nil {
		t.Fatalf("查询 media 列: %v", err)
	}
	if n != 1 {
		t.Fatalf("media.cover_phash 列缺失")
	}
	// 列可正常写入读取（需先建库满足外键）
	if _, err := dbh.Exec(
		`INSERT INTO libraries (name, path, kind) VALUES ('t', 'C:/t', 'mixed')`,
	); err != nil {
		t.Fatalf("创建测试库失败: %v", err)
	}
	if _, err := dbh.Exec(
		`INSERT INTO media (library_id, relative_path, kind, file_size, mtime, cover_phash)
		 VALUES (1, 'v.mp4', 'video', 10, datetime('now'), 'abcd')`,
	); err != nil {
		t.Fatalf("写入 cover_phash 失败: %v", err)
	}
	var ph string
	if err := dbh.QueryRow(`SELECT cover_phash FROM media WHERE relative_path='v.mp4'`).Scan(&ph); err != nil {
		t.Fatalf("读取 cover_phash 失败: %v", err)
	}
	if ph != "abcd" {
		t.Fatalf("cover_phash 内容异常: %q", ph)
	}
}

func TestOpenAppliesWritePragmas(t *testing.T) {
	// WAL 只对文件数据库生效，内存库恒为 memory
	cfg := &config.Config{Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "pragmas.db")}}
	dbh, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbh.Close()

	var syncMode, journalMode, fkMode string
	if err := dbh.QueryRow(`PRAGMA synchronous`).Scan(&syncMode); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	if err := dbh.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if err := dbh.QueryRow(`PRAGMA foreign_keys`).Scan(&fkMode); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if syncMode != "1" && syncMode != "normal" {
		t.Fatalf("synchronous 应为 NORMAL(1)，实际 %q", syncMode)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode 应为 wal，实际 %q", journalMode)
	}
	if fkMode != "1" {
		t.Fatalf("foreign_keys 应为 1，实际 %q", fkMode)
	}
}
