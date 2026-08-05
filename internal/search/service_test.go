// 包 search 测试：以图搜图必须用视频封面帧 pHash（cover_phash）匹配，
// 不能用 sprite pHash（25 帧拼贴图与单帧查询图不可比）；以视频搜视频用
// sprite pHash 匹配视频 + 首帧 pHash 匹配图片，两个距离阈值独立。
// 代码注释使用中文。
package search

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"memable/internal/cmdx"
	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/media"
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

// TestSearchByVideo 验证以视频搜视频：
// 1) sprite pHash 匹配库中视频（含短视频，不区分时长差/<4000ms）；
// 2) 首帧 pHash 只匹配库中图片；
// 3) 两路距离阈值独立生效（imageMaxDistance 不影响视频命中，反之亦然）。
func TestSearchByVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg 未安装，跳过以视频搜视频测试")
	}
	s := newTestService(t)
	lib := &repo.Library{Name: "t", Path: "C:/media", Kind: "mixed"}
	if err := s.Libraries.Create(lib); err != nil {
		t.Fatalf("创建库: %v", err)
	}

	dir := t.TempDir()
	// 视频 A：testsrc2 彩色测试图；视频 B：smptebars 彩条图（内容差异大）
	videoA := filepath.Join(dir, "a.mp4")
	videoB := filepath.Join(dir, "b.mp4")
	for path, src := range map[string]string{
		videoA: "testsrc2=size=320x240:rate=30:d=4",
		videoB: "smptebars=size=320x240:rate=30:d=4",
	} {
		out, err := cmdx.Command(context.Background(), "ffmpeg", "-y", "-f", "lavfi",
			"-i", src,
			"-c:v", "libx264", "-preset", "ultrafast", "-crf", "30",
			"-pix_fmt", "yuv420p", path).CombinedOutput()
		if err != nil {
			t.Fatalf("生成测试视频失败: %v\n%s", err, string(out))
		}
	}

	// 查询视频：视频 A 的 sprite pHash + 首帧 pHash
	meta, err := media.ProbeVideo(context.Background(), videoA)
	if err != nil {
		t.Fatalf("ProbeVideo: %v", err)
	}
	spriteA, err := media.ComputeVideoSpritePHash(context.Background(), videoA, meta.DurationMs)
	if err != nil {
		t.Fatalf("sprite A: %v", err)
	}
	firstDir := t.TempDir()
	firstPath := filepath.Join(firstDir, "first.jpg")
	if err := media.ExtractVideoFirstFrame(context.Background(), videoA, firstPath); err != nil {
		t.Fatalf("首帧: %v", err)
	}
	firstHashes, err := media.ImagePerceptualHashes(firstPath)
	if err != nil {
		t.Fatalf("首帧 pHash: %v", err)
	}

	// 视频 B 的 sprite pHash（应与 A 不同）
	metaB, err := media.ProbeVideo(context.Background(), videoB)
	if err != nil {
		t.Fatalf("ProbeVideo B: %v", err)
	}
	spriteB, err := media.ComputeVideoSpritePHash(context.Background(), videoB, metaB.DurationMs)
	if err != nil {
		t.Fatalf("sprite B: %v", err)
	}

	now := time.Now()
	// 图片：phash == 首帧 pHash（应被首帧命中）
	img := &repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "shot.jpg",
		FileSize: 1, Mtime: now, Phash: &firstHashes.PHash}
	mustUpsert(t, s, img)
	// 视频 A 副本（短视频 2s，验证不区分时长/短视频也命中）：sprite == spriteA
	shortDur := int64(2000)
	vidShort := &repo.Media{LibraryID: lib.ID, Kind: "video", RelativePath: "a_short.mp4",
		FileSize: 2, Mtime: now, Phash: &spriteA, DurationMs: &shortDur}
	mustUpsert(t, s, vidShort)
	// 视频 B：sprite == spriteB（不应被视频 A 命中）
	vidB := &repo.Media{LibraryID: lib.ID, Kind: "video", RelativePath: "b.mp4",
		FileSize: 3, Mtime: now, Phash: &spriteB, DurationMs: &metaB.DurationMs}
	mustUpsert(t, s, vidB)

	results, err := s.SearchByVideo(context.Background(), videoA, 12, 16)
	if err != nil {
		t.Fatalf("SearchByVideo: %v", err)
	}
	hit := map[int64]bool{}
	for _, r := range results {
		hit[r.Media.ID] = true
	}
	if !hit[img.ID] {
		t.Fatalf("首帧应命中图片 shot.jpg，实际 %+v", results)
	}
	if !hit[vidShort.ID] {
		t.Fatalf("sprite 应命中短视频 a_short.mp4（不区分时长差/短视频），实际 %+v", results)
	}
	if hit[vidB.ID] {
		t.Fatalf("视频 B sprite 不同不应命中，实际 %+v", results)
	}

	// 收紧阈值：视频距离 0（仅完全一致）→ 视频 A 副本应仍命中（distance=0），
	// 图片距离 0 → 首帧 pHash 完全相同也应命中
	results, err = s.SearchByVideo(context.Background(), videoA, 0, 0)
	if err != nil {
		t.Fatalf("SearchByVideo(0,0): %v", err)
	}
	hit = map[int64]bool{}
	for _, r := range results {
		hit[r.Media.ID] = true
	}
	if !hit[img.ID] || !hit[vidShort.ID] || hit[vidB.ID] {
		t.Fatalf("阈值 0 时应命中完全一致的图片与短视频，实际 %+v", results)
	}
}
