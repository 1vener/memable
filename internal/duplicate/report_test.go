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
	duration := int64(1000)

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
