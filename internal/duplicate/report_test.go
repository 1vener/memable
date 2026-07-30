// report_test.go：应用内重复报告结构测试。
package duplicate

import (
	"path/filepath"
	"testing"

	"memable/internal/repo"
)

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
