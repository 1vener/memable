// settings_repo_test.go：settings 杂项参数表 CRUD 测试。
// 代码注释使用中文。
package repo

import (
	"testing"

	"memable/internal/config"
	"memable/internal/db"
)

func TestSettingsRepoCRUD(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	r := NewSettingsRepo(dbh)

	// 不存在时返回空
	v, err := r.Get("netdrive.cd2.token")
	if err != nil || v != "" {
		t.Fatalf("空参数应返回空串: %q %v", v, err)
	}

	// 写入/读取
	if err := r.Set("netdrive.cd2.token", "tok-abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err = r.Get("netdrive.cd2.token")
	if err != nil || v != "tok-abc" {
		t.Fatalf("读取不符: %q %v", v, err)
	}

	// 覆盖写入
	if err := r.Set("netdrive.cd2.token", "tok-def"); err != nil {
		t.Fatalf("Set 覆盖: %v", err)
	}
	v, _ = r.Get("netdrive.cd2.token")
	if v != "tok-def" {
		t.Fatalf("覆盖后不符: %q", v)
	}

	// 列表
	if err := r.Set("other.key", "x"); err != nil {
		t.Fatal(err)
	}
	entries, err := r.List()
	if err != nil || len(entries) != 2 {
		t.Fatalf("列表不符: %v %v", entries, err)
	}

	// 删除
	if err := r.Delete("netdrive.cd2.token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	v, _ = r.Get("netdrive.cd2.token")
	if v != "" {
		t.Fatalf("删除后应为空: %q", v)
	}
}
