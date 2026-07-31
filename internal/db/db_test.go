// 包 db 测试：内存库上验证 Open + Migrate。
// 代码注释使用中文。
package db

import (
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
	if v != 1 {
		t.Fatalf("expected version 1, got %d", v)
	}

	// 验证核心表已建立；video_frames 已在 v2 迁移中移除
	for _, tbl := range []string{"libraries", "scan_sessions", "media", "file_stats"} {
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
	if _, err := dbh.Exec(`UPDATE schema_version SET version=99`); err != nil {
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
