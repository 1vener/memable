// netdrive_test.go：杂项参数（kv）接口与 CloudDrive2 补齐 SHA1 任务提交测试。
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
		`{"key":"netdrive.cd2.token","value":"tok-abc"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("写入 kv: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 读取列表应含刚写入的键
	rec, _ = doJSON(t, server, http.MethodGet, "/api/settings/kv", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "netdrive.cd2.token") {
		t.Fatalf("kv 列表应含刚写入的键: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 删除
	rec, _ = doJSON(t, server, http.MethodDelete, "/api/settings/kv",
		`{"key":"netdrive.cd2.token"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("删除 kv: code=%d", rec.Code)
	}
	// 验证已删除
	rec, _ = doJSON(t, server, http.MethodGet, "/api/settings/kv", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "netdrive.cd2.token") {
		t.Fatalf("kv 列表应已清空: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestNetdriveSyncRequiresToken 未配置 CD2 API Token 时提交任务应 400。
func TestNetdriveSyncRequiresToken(t *testing.T) {
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

	rec, _ := doJSON(t, server, http.MethodPost, "/api/netdrive/cd2/sync-sha1",
		`{"library_id":`+formatInt64(lib.ID)+`,"local_dir":"videos","remote_path":"/115/视频"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未配置 Token 应 400: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
