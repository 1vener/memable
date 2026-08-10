// media_file_test.go：GET /api/media/{id}/file 源文件字节接口测试
// （ServeContent 全文/Range、路径越界防护）。
// 代码注释使用中文。
package api

import (
	"io"
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

func setupMediaFileServer(t *testing.T) (*Server, *repo.MediaRepo, int64, string) {
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
	mr := repo.NewMediaRepo(dbh)

	root := t.TempDir()
	lib := &repo.Library{Name: "源文件库", Path: root, Kind: "mixed"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	// 真实文件：sub/a.jpg
	fileAbs := filepath.Join(root, "sub", "a.jpg")
	if err := os.MkdirAll(filepath.Dir(fileAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte(strings.Repeat("jpeg-data-", 100)) // 1000 字节
	if err := os.WriteFile(fileAbs, content, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	media := &repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "sub/a.jpg", FileSize: int64(len(content)), Mtime: now}
	if err := mr.Upsert(media); err != nil {
		t.Fatal(err)
	}

	server := NewServer(cfg, lr, nil, mr, nil, nil, nil, nil, nil, nil, "", "", nil)
	return server, mr, lib.ID, root
}

func TestHandleMediaFileFullAndRange(t *testing.T) {
	server, mr, libID, _ := setupMediaFileServer(t)
	medias, err := mr.ListByLibrary(libID)
	if err != nil || len(medias) != 1 {
		t.Fatalf("读取媒体失败: %v", err)
	}
	id := medias[0].ID
	url := "/api/media/" + formatInt64(id) + "/file"

	// 1. 全文请求
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("全文请求 code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 1000 {
		t.Fatalf("全文长度 = %d, want 1000", rec.Body.Len())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "image/jpeg") {
		t.Fatalf("Content-Type = %q, want image/jpeg", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", cd)
	}

	// 2. Range 请求（视频拖动进度依赖）
	req = httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=100-199")
	rec = httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("Range 请求 code=%d, want 206", rec.Code)
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 100-199/1000" {
		t.Fatalf("Content-Range = %q, want bytes 100-199/1000", cr)
	}
	body, _ := io.ReadAll(rec.Body)
	if len(body) != 100 {
		t.Fatalf("Range 响应体长度 = %d, want 100", len(body))
	}
}

func TestHandleMediaFileInvalid(t *testing.T) {
	server, mr, libID, _ := setupMediaFileServer(t)

	// 1. 媒体不存在
	req := httptest.NewRequest(http.MethodGet, "/api/media/99999/file", nil)
	rec := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在媒体 code=%d, want 404", rec.Code)
	}

	// 2. 相对路径越界（../ 逃出库根）
	evil := &repo.Media{LibraryID: libID, Kind: "image", RelativePath: "../evil.jpg", FileSize: 1, Mtime: time.Now().UTC()}
	if err := mr.Upsert(evil); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/media/"+formatInt64(evil.ID)+"/file", nil)
	rec = httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("越界路径 code=%d, want 403", rec.Code)
	}
}
