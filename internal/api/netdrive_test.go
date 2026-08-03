// netdrive_test.go：杂项参数（kv）接口与 115 补齐 SHA1 任务提交测试。
// 代码注释使用中文。
package api

import (
	"net/http"
	"strings"
	"testing"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

func TestSettingsKVAPI(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	lr := repo.NewLibraryRepo(dbh)
	sr := repo.NewSessionRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	tr := repo.NewTaskRepo(dbh)
	settings := repo.NewSettingsRepo(dbh)
	server := NewServer(cfg, lr, sr, mr, tr, nil, settings, nil, nil, nil, "", "", nil)

	// 写入
	rec, _ := doJSON(t, server, http.MethodPut, "/api/settings/kv",
		`{"key":"netdrive.115.cookie","value":"UID=1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("写入 kv: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 读取列表应含刚写入的键
	rec, _ = doJSON(t, server, http.MethodGet, "/api/settings/kv", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "netdrive.115.cookie") {
		t.Fatalf("kv 列表应含刚写入的键: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 删除
	rec, _ = doJSON(t, server, http.MethodDelete, "/api/settings/kv",
		`{"key":"netdrive.115.cookie"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("删除 kv: code=%d", rec.Code)
	}
	// 验证已删除
	rec, _ = doJSON(t, server, http.MethodGet, "/api/settings/kv", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "netdrive.115.cookie") {
		t.Fatalf("kv 列表应已清空: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestNetdriveSyncRequiresCookie 未配置 115 Cookie 时提交任务应 400。
func TestNetdriveSyncRequiresCookie(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	lr := repo.NewLibraryRepo(dbh)
	sr := repo.NewSessionRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	tr := repo.NewTaskRepo(dbh)
	settings := repo.NewSettingsRepo(dbh)
	server := NewServer(cfg, lr, sr, mr, tr, nil, settings, nil, nil, nil, "", "", nil)

	lib := &repo.Library{Name: "库", Path: "C:/media", Kind: "video"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}

	rec, _ := doJSON(t, server, http.MethodPost, "/api/netdrive/115/sync-sha1",
		`{"library_id":`+formatInt64(lib.ID)+`,"local_dir":"videos","remote_cid":"10"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未配置 Cookie 应 400: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
