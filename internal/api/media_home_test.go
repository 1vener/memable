// media_home_test.go：首页媒体 HTTP 接口测试。
// 代码注释使用中文。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

func setupMediaHomeServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	d, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	lr := repo.NewLibraryRepo(d)
	lib := &repo.Library{Name: "首页库", Path: "C:/home", Kind: "mixed"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	mr := repo.NewMediaRepo(d)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	duration := int64(3000)
	for i, m := range []repo.Media{
		{LibraryID: lib.ID, Kind: "image", RelativePath: "目录/a.jpg", FileSize: 10, Mtime: base.Add(2 * time.Hour)},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "目录/子目录/b.jpg", FileSize: 20, Mtime: base.Add(time.Hour)},
		{LibraryID: lib.ID, Kind: "video", RelativePath: "v.mp4", FileSize: 30, Mtime: base, DurationMs: &duration},
	} {
		if err := mr.Upsert(&m); err != nil {
			t.Fatalf("写入媒体 %d: %v", i, err)
		}
	}
	return NewServer(cfg, lr, repo.NewSessionRepo(d), mr, nil, nil, nil, nil, nil, nil, "", "", nil)
}

func TestMediaHomeEndpoints(t *testing.T) {
	s := setupMediaHomeServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/media?kind=image&page=1&page_size=1", nil)
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("媒体分页状态=%d body=%s", w.Code, w.Body.String())
	}
	var page repo.MediaPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].RelativePath != "目录/a.jpg" {
		t.Fatalf("媒体分页错误: %+v", page)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/media/groups?depth=1&offset=0&limit=1", nil)
	w = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, req)
	var grouped struct {
		Total int               `json:"total"`
		Items []repo.MediaGroup `json:"items"`
	}
	if w.Code != http.StatusOK {
		t.Fatalf("分组状态=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &grouped); err != nil {
		t.Fatal(err)
	}
	if grouped.Total != 2 || len(grouped.Items) != 1 || grouped.Items[0].Total != 2 || len(grouped.Items[0].Items) != 2 {
		t.Fatalf("目录分组错误: %+v", grouped)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/media/statistics", nil)
	w = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, req)
	var stats repo.MediaStatistics
	if w.Code != http.StatusOK {
		t.Fatalf("统计状态=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Image.Count != 2 || stats.Video.Count != 1 || stats.Video.DurationMs != 3000 || stats.TotalSize != 60 {
		t.Fatalf("统计错误: %+v", stats)
	}
}

func TestMediaHomeRejectsInvalidParameters(t *testing.T) {
	s := setupMediaHomeServer(t)
	for _, target := range []string{"/api/media?kind=all", "/api/media?kind=image&page=x", "/api/media/groups?depth=x"} {
		w := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s 状态=%d, want 400", target, w.Code)
		}
	}
}
