// dir_rename_move_test.go：目录重命名/移动接口测试（真实建盘，验证磁盘与数据库同步）。
// 代码注释使用中文。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

// setupDirOpServer 建临时库根目录 + 目录结构 dir1/sub/file.jpg + 对应 DB 记录。
func setupDirOpServer(t *testing.T) (*Server, *repo.MediaRepo, int64, string) {
	t.Helper()
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	lr := repo.NewLibraryRepo(dbh)
	sr := repo.NewSessionRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	tr := repo.NewTaskRepo(dbh)

	root := t.TempDir()
	lib := &repo.Library{Name: "测试库", Path: root, Kind: "mixed"}
	if err := lr.Create(lib); err != nil {
		t.Fatalf("创建库: %v", err)
	}
	// 建盘：root/dir1/sub/file.jpg
	fileAbs := filepath.Join(root, "dir1", "sub", "file.jpg")
	if err := os.MkdirAll(filepath.Dir(fileAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileAbs, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 入库
	now := time.Now().UTC()
	if err := mr.Upsert(&repo.Media{
		LibraryID: lib.ID, Kind: "image", RelativePath: "dir1/sub/file.jpg",
		FileSize: 4, Mtime: now,
	}); err != nil {
		t.Fatalf("写入媒体记录: %v", err)
	}

	server := NewServer(cfg, lr, sr, mr, tr, nil, nil, nil, nil, nil, "", "", nil)
	return server, mr, lib.ID, root
}

func doJSON(t *testing.T, server *Server, method, url string, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func dbRels(t *testing.T, mr *repo.MediaRepo, libID int64) []string {
	t.Helper()
	list, err := mr.ListByLibrary(libID)
	if err != nil {
		t.Fatalf("查询媒体: %v", err)
	}
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.RelativePath)
	}
	return out
}

// TestRenameDirectoryAPI 重命名成功：磁盘目录改名 + 数据库相对路径同步更新。
func TestRenameDirectoryAPI(t *testing.T) {
	server, mr, libID, root := setupDirOpServer(t)
	rec, out := doJSON(t, server, http.MethodPost,
		"/api/libraries/"+formatInt64(libID)+"/directories/rename",
		`{"path":"dir1","new_name":"dir2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := out["renamed_media"]; n != float64(1) {
		t.Fatalf("renamed_media=%v, want 1", n)
	}
	// 磁盘
	if _, err := os.Stat(filepath.Join(root, "dir2", "sub", "file.jpg")); err != nil {
		t.Fatalf("磁盘新路径不存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir1")); !os.IsNotExist(err) {
		t.Fatalf("磁盘旧路径应已消失")
	}
	// 数据库
	rels := dbRels(t, mr, libID)
	if len(rels) != 1 || rels[0] != "dir2/sub/file.jpg" {
		t.Fatalf("数据库路径未同步: %v", rels)
	}
}

// TestRenameDirectoryErrors 重命名非法场景：根目录/未变化/非法名称/同名冲突。
func TestRenameDirectoryErrors(t *testing.T) {
	server, _, libID, root := setupDirOpServer(t)
	base := "/api/libraries/" + formatInt64(libID) + "/directories/rename"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"根目录", `{"path":"","new_name":"x"}`, 400},
		{"未变化", `{"path":"dir1","new_name":"dir1"}`, 400},
		{"非法字符", `{"path":"dir1","new_name":"a/b"}`, 400},
		{"目录不存在", `{"path":"nope","new_name":"x"}`, 404},
	}
	for _, c := range cases {
		rec, _ := doJSON(t, server, http.MethodPost, base, c.body)
		if rec.Code != c.want {
			t.Errorf("%s: 状态码=%d, want %d (body=%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	// 已存在同名目录 → 409
	if err := os.MkdirAll(filepath.Join(root, "dir2"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, _ := doJSON(t, server, http.MethodPost, base, `{"path":"dir1","new_name":"dir2"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("同名冲突: 状态码=%d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMoveDirectoryAPI 移动目录成功：dir1/sub 移到根目录 → sub/...。
func TestMoveDirectoryAPI(t *testing.T) {
	server, mr, libID, root := setupDirOpServer(t)
	rec, out := doJSON(t, server, http.MethodPost,
		"/api/libraries/"+formatInt64(libID)+"/directories/move",
		`{"path":"dir1/sub","target_dir":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := out["moved_media"]; n != float64(1) {
		t.Fatalf("moved_media=%v, want 1", n)
	}
	// 磁盘
	if _, err := os.Stat(filepath.Join(root, "sub", "file.jpg")); err != nil {
		t.Fatalf("磁盘新路径不存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir1", "sub")); !os.IsNotExist(err) {
		t.Fatalf("磁盘旧路径应已消失")
	}
	// 数据库
	rels := dbRels(t, mr, libID)
	if len(rels) != 1 || rels[0] != "sub/file.jpg" {
		t.Fatalf("数据库路径未同步: %v", rels)
	}
}

// TestMoveDirectoryErrors 移动非法场景：自身子孙/目标已存在。
func TestMoveDirectoryErrors(t *testing.T) {
	server, _, libID, root := setupDirOpServer(t)
	base := "/api/libraries/" + formatInt64(libID) + "/directories/move"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"移动到自身", `{"path":"dir1","target_dir":"dir1"}`, 400},
		{"移动到子孙", `{"path":"dir1","target_dir":"dir1/sub"}`, 400},
		{"根目录", `{"path":"","target_dir":""}`, 400},
	}
	for _, c := range cases {
		rec, _ := doJSON(t, server, http.MethodPost, base, c.body)
		if rec.Code != c.want {
			t.Errorf("%s: 状态码=%d, want %d (body=%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	// 目标已存在同名目录 → 409：把 dir1/sub 移到 dir1 下冲突（target_dir=dir1 会被"自身子孙"拦截），
	// 改用 dir1/sub 移到根 → 已存在 sub？不存在。改为先建同名目录 dir1/sub2 → move dir1/sub target_dir=dir1/sub2/.. 不行。
	// 直接构造：在根建 sub 目录，再把 dir1/sub 移到根 → 409
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, _ := doJSON(t, server, http.MethodPost, base, `{"path":"dir1/sub","target_dir":""}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("目标已存在: 状态码=%d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestRenameDirectoryCaseOnly Windows 大小写改名（dir1 → DIR1）两步走生效。
func TestRenameDirectoryCaseOnly(t *testing.T) {
	server, mr, libID, root := setupDirOpServer(t)
	rec, _ := doJSON(t, server, http.MethodPost,
		"/api/libraries/"+formatInt64(libID)+"/directories/rename",
		`{"path":"dir1","new_name":"DIR1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "DIR1", "sub", "file.jpg")); err != nil {
		t.Fatalf("大小写改名后新路径不存在: %v", err)
	}
	rels := dbRels(t, mr, libID)
	if len(rels) != 1 || rels[0] != "DIR1/sub/file.jpg" {
		t.Fatalf("数据库路径未同步: %v", rels)
	}
}
