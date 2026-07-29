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
	if v != 3 {
		t.Fatalf("expected version 3, got %d", v)
	}

	// 验证核心表已建立；video_frames 已在 v2 迁移中移除
	for _, tbl := range []string{"libraries", "scan_sessions", "media"} {
		var n int
		q := "SELECT count(*) FROM " + tbl
		if err := dbh.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("表 %s 不存在或查询失败: %v", tbl, err)
		}
	}
}
