// 包 search 测试：以图搜图必须用视频封面帧 pHash（cover_phash）匹配，
// 不能用 sprite pHash（25 帧拼贴图与单帧查询图不可比）。
// 代码注释使用中文。
package search

import (
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewService(repo.NewMediaRepo(dbh), repo.NewLibraryRepo(dbh))
}

func mustUpsert(t *testing.T, s *Service, m *repo.Media) {
	t.Helper()
	if err := s.Media.Upsert(m); err != nil {
		t.Fatalf("Upsert %q: %v", m.RelativePath, err)
	}
}

// TestSearchByImageMatchesVideoCoverPhash 验证：
// 1) 查询图 pHash 与视频 cover_phash 相同（距离 0）时能匹配到视频；
// 2) 视频 cover_phash 缺失时被跳过；
// 3) 视频不会拿 sprite phash 参与匹配（cover 与 sprite 不同时查 sprite 无结果）。
func TestSearchByImageMatchesVideoCoverPhash(t *testing.T) {
	s := newTestService(t)
	lib := &repo.Library{Name: "t", Path: "C:/media", Kind: "mixed"}
	if err := s.Libraries.Create(lib); err != nil {
		t.Fatalf("创建库: %v", err)
	}

	imgPHash := "1111111111111111"
	coverPHash := "2222222222222222"  // 视频封面帧 pHash
	spritePHash := "3333333333333333" // 视频 sprite 拼贴 pHash（与单帧不可比）
	now := time.Now()
	img := &repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "a.jpg",
		FileSize: 1, Mtime: now, Phash: &imgPHash}
	mustUpsert(t, s, img)
	vid := &repo.Media{LibraryID: lib.ID, Kind: "video", RelativePath: "b.mp4",
		FileSize: 2, Mtime: now, Phash: &spritePHash, CoverPHash: &coverPHash}
	mustUpsert(t, s, vid)
	vidNoCover := &repo.Media{LibraryID: lib.ID, Kind: "video", RelativePath: "c.mp4",
		FileSize: 3, Mtime: now, Phash: &spritePHash, CoverPHash: nil}
	mustUpsert(t, s, vidNoCover)

	// 用封面 pHash 搜索：应命中视频 b，且不命中图片 a
	results, err := s.SearchByImage(coverPHash, 12)
	if err != nil {
		t.Fatalf("SearchByImage: %v", err)
	}
	if len(results) != 1 || results[0].Media.ID != vid.ID {
		t.Fatalf("按封面 pHash 搜索应只命中视频 b，实际 %+v", results)
	}

	// 用 sprite pHash 搜索：不得命中任何视频（sprite 与单帧不可比）
	results, err = s.SearchByImage(spritePHash, 12)
	if err != nil {
		t.Fatalf("SearchByImage: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("按 sprite pHash 搜索不应命中，实际 %+v", results)
	}

	// 用图片 pHash 搜索：命中图片 a
	results, err = s.SearchByImage(imgPHash, 12)
	if err != nil {
		t.Fatalf("SearchByImage: %v", err)
	}
	if len(results) != 1 || results[0].Media.ID != img.ID {
		t.Fatalf("按图片 pHash 搜索应命中图片 a，实际 %+v", results)
	}
}
