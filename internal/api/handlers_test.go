// handlers_test.go：HTTP API 收藏库删除行为测试。
// 代码注释使用中文。
package api

import (
	"fmt"
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

func TestPercentFromDistance(t *testing.T) {
	cases := []struct {
		dist int
		want int
	}{
		{10, 84}, // 旧版配置默认距离 10 → 84%
		{6, 91},
		{0, 90},
		{64, 0},
	}
	for _, c := range cases {
		if got := percentFromDistance(c.dist); got != c.want {
			t.Errorf("percentFromDistance(%d) = %d, want %d", c.dist, got, c.want)
		}
	}
}

func TestDeleteLibraryRemovesRelatedDataAndUnreferencedThumbnails(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}

	libraries := repo.NewLibraryRepo(dbh)
	sessions := repo.NewSessionRepo(dbh)
	mediaRepo := repo.NewMediaRepo(dbh)
	deletedLibrary := &repo.Library{Name: "待删除库", Path: "D:/Deleted", Kind: "image"}
	keptLibrary := &repo.Library{Name: "保留库", Path: "D:/Kept", Kind: "image"}
	if err := libraries.Create(deletedLibrary); err != nil {
		t.Fatal(err)
	}
	if err := libraries.Create(keptLibrary); err != nil {
		t.Fatal(err)
	}

	sessionID := "delete-session"
	if err := sessions.Create(&repo.ScanSession{
		ID: sessionID, LibraryID: &deletedLibrary.ID, Status: "completed",
	}); err != nil {
		t.Fatal(err)
	}

	uniqueThumb := "aa/unique.png"
	sharedThumb := "bb/shared.png"
	now := time.Now().UTC()
	items := []repo.Media{
		{LibraryID: deletedLibrary.ID, ScanSessionID: &sessionID, Kind: "image", RelativePath: "a.jpg", FileSize: 1, Mtime: now, ThumbnailPath: &uniqueThumb},
		{LibraryID: deletedLibrary.ID, ScanSessionID: &sessionID, Kind: "image", RelativePath: "b.jpg", FileSize: 1, Mtime: now, ThumbnailPath: &uniqueThumb},
		{LibraryID: deletedLibrary.ID, ScanSessionID: &sessionID, Kind: "image", RelativePath: "shared.jpg", FileSize: 1, Mtime: now, ThumbnailPath: &sharedThumb},
		{LibraryID: keptLibrary.ID, Kind: "image", RelativePath: "shared.jpg", FileSize: 1, Mtime: now, ThumbnailPath: &sharedThumb},
	}
	for i := range items {
		if err := mediaRepo.Upsert(&items[i]); err != nil {
			t.Fatalf("写入媒体记录: %v", err)
		}
	}

	thumbBase := t.TempDir()
	for _, rel := range []string{uniqueThumb, sharedThumb} {
		path := filepath.Join(thumbBase, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("thumbnail"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := NewServer(cfg, libraries, sessions, mediaRepo, nil, nil, nil, nil, nil, thumbBase, thumbBase, nil)
	request := httptest.NewRequest(http.MethodDelete, "/api/libraries/"+formatInt64(deletedLibrary.ID), nil)
	response := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("删除接口状态码=%d, body=%s", response.Code, response.Body.String())
	}

	if _, err := libraries.GetByID(deletedLibrary.ID); err == nil {
		t.Fatal("收藏库记录未删除")
	}
	deletedMedia, err := mediaRepo.ListByLibrary(deletedLibrary.ID)
	if err != nil || len(deletedMedia) != 0 {
		t.Fatalf("关联媒体记录未删除: count=%d err=%v", len(deletedMedia), err)
	}
	if _, err := sessions.GetByID(sessionID); err == nil {
		t.Fatal("关联扫描会话未删除")
	}
	if _, err := os.Stat(filepath.Join(thumbBase, filepath.FromSlash(uniqueThumb))); !os.IsNotExist(err) {
		t.Fatalf("无人引用的缩略图未删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(thumbBase, filepath.FromSlash(sharedThumb))); err != nil {
		t.Fatalf("其他库引用的缩略图不应删除: %v", err)
	}
	keptMedia, err := mediaRepo.ListByLibrary(keptLibrary.ID)
	if err != nil || len(keptMedia) != 1 {
		t.Fatalf("其他库数据受影响: count=%d err=%v", len(keptMedia), err)
	}
}

func formatInt64(value int64) string {
	return fmt.Sprintf("%d", value)
}

func TestOpenMediaFileValid(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(imgPath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := &repo.Library{Name: "库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	m := &repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "photo.jpg", FileSize: 4, Mtime: time.Now().UTC()}
	if err := mr.Upsert(m); err != nil {
		t.Fatal(err)
	}
	server := NewServer(cfg, lr, sr, mr, nil, nil, nil, nil, nil, "", "", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/media/"+formatInt64(m.ID)+"/open",
		body(`{"action":"file"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	// 平台不支持时返回500，否则200（实际执行打开命令）
	if resp.Code != http.StatusOK && resp.Code != http.StatusInternalServerError {
		t.Fatalf("状态码=%d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestOpenMediaNotFound(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}
	server := NewServer(cfg, repo.NewLibraryRepo(dbh), repo.NewSessionRepo(dbh),
		repo.NewMediaRepo(dbh), nil, nil, nil, nil, nil, "", "", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/media/99999/open",
		body(`{"action":"file"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("应返回404: code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestOpenMediaFileNotExistOnDisk(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	lib := &repo.Library{Name: "库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	m := &repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "nonexistent.jpg", FileSize: 4, Mtime: time.Now().UTC()}
	if err := mr.Upsert(m); err != nil {
		t.Fatal(err)
	}
	server := NewServer(cfg, lr, sr, mr, nil, nil, nil, nil, nil, "", "", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/media/"+formatInt64(m.ID)+"/open",
		body(`{"action":"file"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("磁盘文件不存在应返回404: code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestOpenMediaDirectoryAction(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(imgPath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := &repo.Library{Name: "库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	m := &repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "photo.jpg", FileSize: 4, Mtime: time.Now().UTC()}
	if err := mr.Upsert(m); err != nil {
		t.Fatal(err)
	}
	server := NewServer(cfg, lr, sr, mr, nil, nil, nil, nil, nil, "", "", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/media/"+formatInt64(m.ID)+"/open",
		body(`{"action":"directory"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK && resp.Code != http.StatusInternalServerError {
		t.Fatalf("状态码=%d, body=%s", resp.Code, resp.Body.String())
	}
}

func body(s string) *strings.Reader {
	return strings.NewReader(s)
}
