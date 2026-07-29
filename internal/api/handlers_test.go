// handlers_test.go：HTTP API 收藏库删除行为测试。
// 代码注释使用中文。
package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

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

	uniqueThumb := "image/aa/unique.png"
	sharedThumb := "image/bb/shared.png"
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

	server := NewServer(cfg, libraries, sessions, mediaRepo, nil, nil, nil, nil, thumbBase)
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
