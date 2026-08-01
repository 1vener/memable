// report_test.go：应用内重复报告结构测试。
package duplicate

import (
	"path/filepath"
	"reflect"
	"testing"

	"memable/internal/repo"
)

func TestDistanceFromPercent(t *testing.T) {
	cases := []struct {
		percent int
		want    int
	}{
		{100, 0},
		{90, 6},
		{0, 64},
		{50, 32},
	}
	for _, c := range cases {
		if got := distanceFromPercent(c.percent); got != c.want {
			t.Errorf("distanceFromPercent(%d) = %d, want %d", c.percent, got, c.want)
		}
	}
}

func TestFilterSameDir(t *testing.T) {
	groups := []Group{
		{Reason: "sha1_exact", Media: []repo.Media{
			{RelativePath: "a/one.jpg"},
			{RelativePath: "a/two.jpg"},
		}},
		{Reason: "sha1_exact", Media: []repo.Media{
			{RelativePath: "a/one.jpg"},
			{RelativePath: "b/two.jpg"},
		}},
		{Reason: "sha1_exact", Media: []repo.Media{
			{RelativePath: "one.jpg"},
			{RelativePath: "two.jpg"},
		}},
	}
	filtered := filterSameDir(groups)
	if len(filtered) != 2 {
		t.Fatalf("应保留 2 个同目录组，实际 %d", len(filtered))
	}
	var dirs []string
	for _, g := range filtered {
		dirs = append(dirs, relDir(g.Media[0].RelativePath))
	}
	if !reflect.DeepEqual(dirs, []string{"a", "."}) {
		t.Fatalf("目录列表不符合预期: %v", dirs)
	}
}

func TestVideoPHashDoesNotHardFilterDifferentOshash(t *testing.T) {
	firstHash := "0000000000000000"
	secondHash := "0000000000000000"
	firstOshash := "oshash-a"
	secondOshash := "oshash-b"
	// 普通视频仍需验证 oshash 不能作为 pHash 硬过滤条件。
	duration := int64(60000)

	groups := (&Detector{}).detectVideoPHashSimilarDist([]repo.Media{
		{
			ID: 1, Kind: "video", Phash: &firstHash,
			Oshash: &firstOshash, DurationMs: &duration,
		},
		{
			ID: 2, Kind: "video", Phash: &secondHash,
			Oshash: &secondOshash, DurationMs: &duration,
		},
	}, 0, 0, true)

	if len(groups) != 1 || len(groups[0].Media) != 2 {
		t.Fatalf("不同 oshash 的视觉相同视频不应被硬过滤: %+v", groups)
	}
}

func TestBetterKeep(t *testing.T) {
	older := repo.MediaView{Media: repo.Media{FileSize: 10, RelativePath: "x.jpg"}}
	newer := repo.MediaView{Media: repo.Media{FileSize: 20, RelativePath: "longer-name.jpg"}}
	if !betterKeep(newer, older, "largest") {
		t.Error("largest 应选更大文件")
	}
	if betterKeep(newer, older, "smallest") {
		t.Error("smallest 应选更小文件")
	}
	if !betterKeep(newer, older, "longest_name") {
		t.Error("longest_name 应选更长文件名")
	}
}

// TestVideoPHashGroupsAcrossDifferentOshash 回归：oshash 粗筛不得作为硬过滤，
// oshash 不同但 sprite pHash/时长相近的视频仍应成组。
func TestVideoPHashGroupsAcrossDifferentOshash(t *testing.T) {
	ph := "0123456789abcdef"
	dur := int64(60000)
	items := []repo.Media{
		{ID: 1, Kind: "video", Phash: &ph, DurationMs: &dur, Oshash: strp("aaa")},
		{ID: 2, Kind: "video", Phash: &ph, DurationMs: &dur, Oshash: strp("bbb")},
		{ID: 3, Kind: "video", Phash: &ph, DurationMs: &dur, Oshash: strp("ccc")},
	}
	d := &Detector{}
	groups := d.detectVideoPHashSimilarDist(items, 12, 3000, true)
	if len(groups) != 1 || len(groups[0].Media) != 3 {
		t.Fatalf("oshash 不同的视频也应合并为 1 组 3 个，实际 %d 组", len(groups))
	}
}

// TestShortVideoUsesSha1OrOshashWithoutPHash 验证短视频使用 SHA1/OSHash 的 OR 关系，
// 不再因为 sprite pHash 相同而误报。
func TestShortVideoUsesSha1OrOshashWithoutPHash(t *testing.T) {
	dur := int64(3999)
	items := []repo.Media{
		// pHash 不同但 SHA1 相同，仍应成组。
		{ID: 1, Kind: "video", Phash: strp("0000000000000000"), DurationMs: &dur, Sha1: strp("sha-a"), Oshash: strp("osh-a")},
		{ID: 2, Kind: "video", Phash: strp("ffffffffffffffff"), DurationMs: &dur, Sha1: strp("sha-a"), Oshash: strp("osh-b")},
		// SHA1 不同但 OSHash 相同，仍应成组。
		{ID: 3, Kind: "video", Phash: strp("0000000000000000"), DurationMs: &dur, Sha1: strp("sha-c"), Oshash: strp("osh-c")},
		{ID: 4, Kind: "video", Phash: strp("ffffffffffffffff"), DurationMs: &dur, Sha1: strp("sha-d"), Oshash: strp("osh-c")},
		// SHA1/OSHash 均不同，即使 pHash 相同也不能成组。
		{ID: 5, Kind: "video", Phash: strp("0000000000000000"), DurationMs: &dur, Sha1: strp("sha-e"), Oshash: strp("osh-e")},
	}

	groups := (&Detector{}).detectVideos(items, Options{
		MediaType: "video", IncludeSHA1: true,
		VideoPhashDistance: 0, VideoDurationDiffMs: 0,
	})
	if len(groups) != 2 {
		t.Fatalf("短视频应只有 SHA1/OSHash 两组，实际 %+v", groups)
	}

	seen := map[string][]int64{}
	for _, g := range groups {
		ids := make([]int64, 0, len(g.Media))
		for _, m := range g.Media {
			ids = append(ids, m.ID)
		}
		seen[g.Reason] = ids
	}
	if got := seen["sha1_exact"]; len(got) != 2 || !containsIDs(got, 1, 2) {
		t.Fatalf("SHA1 短视频组不符: %+v", seen)
	}
	if got := seen["oshash_short_exact"]; len(got) != 2 || !containsIDs(got, 3, 4) {
		t.Fatalf("OSHash 短视频组不符: %+v", seen)
	}
}

// TestShortVideoHashRelationsAreTransitive 验证 SHA1/OSHash OR 关系通过并查集传递合并。
func TestShortVideoHashRelationsAreTransitive(t *testing.T) {
	dur := int64(1000)
	items := []repo.Media{
		{ID: 1, Kind: "video", DurationMs: &dur, Sha1: strp("sha-a"), Oshash: strp("osh-a")},
		{ID: 2, Kind: "video", DurationMs: &dur, Sha1: strp("sha-a"), Oshash: strp("osh-b")},
		{ID: 3, Kind: "video", DurationMs: &dur, Sha1: strp("sha-c"), Oshash: strp("osh-b")},
	}
	groups := (&Detector{}).detectVideos(items, Options{MediaType: "video", IncludeSHA1: true})
	if len(groups) != 1 || len(groups[0].Media) != 3 || groups[0].Reason != "sha1_exact" {
		t.Fatalf("SHA1/OSHash 关系应传递合并: %+v", groups)
	}
}

// TestFourSecondVideoStillUsesPHash 验证 4000ms 恰好进入普通视频 pHash 路径。
func TestFourSecondVideoStillUsesPHash(t *testing.T) {
	dur := int64(4000)
	items := []repo.Media{
		{ID: 1, Kind: "video", Phash: strp("0000000000000000"), DurationMs: &dur, Sha1: strp("sha-a"), Oshash: strp("osh-a")},
		{ID: 2, Kind: "video", Phash: strp("0000000000000000"), DurationMs: &dur, Sha1: strp("sha-b"), Oshash: strp("osh-b")},
	}
	groups := (&Detector{}).detectVideos(items, Options{
		MediaType: "video", IncludeSHA1: true,
		VideoPhashDistance: 0, VideoDurationDiffMs: 0,
	})
	if len(groups) != 1 || groups[0].Reason != "sprite_phash_similar" {
		t.Fatalf("4000ms 视频应走 pHash 路径: %+v", groups)
	}
}

func containsIDs(ids []int64, want ...int64) bool {
	if len(ids) != len(want) {
		return false
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for _, id := range want {
		if !set[id] {
			return false
		}
	}
	return true
}

func strp(s string) *string { return &s }

func TestBuildReportIncludesPathsAndMediaDetails(t *testing.T) {
	thumb := "image/aa/thumb.png"
	width, height := 1920, 1080
	groups := []Group{{
		Kind:   "image",
		Reason: "sha1_exact",
		Media: []repo.Media{
			{ID: 1, LibraryID: 7, Kind: "image", RelativePath: "a/one.jpg", FileSize: 10, Width: &width, Height: &height, ThumbnailPath: &thumb},
			{ID: 2, LibraryID: 7, Kind: "image", RelativePath: "b/two.jpg", FileSize: 20},
		},
	}}
	libraries := []repo.Library{{ID: 7, Path: filepath.Join("root", "media")}}

	report := BuildReport(groups, libraries, "image")
	if report.Kind != "image" || report.GroupCount != 1 || report.FileCount != 2 {
		t.Fatalf("报告统计错误: %+v", report)
	}
	group := report.Groups[0]
	if group.Reason != "SHA1 完全相同" || len(group.Items) != 2 {
		t.Fatalf("报告分组错误: %+v", group)
	}
	item := group.Items[0]
	if item.ID != 1 || item.ThumbnailPath != thumb || item.Width != width || item.Height != height {
		t.Fatalf("媒体详情错误: %+v", item)
	}
	if len(item.DuplicatePaths) != 2 || item.DuplicatePaths[0] == "" || item.DuplicatePaths[1] == "" {
		t.Fatalf("重复文件完整路径缺失: %+v", item.DuplicatePaths)
	}
}
