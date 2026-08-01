// report.go：构建应用内重复检测报告数据。
// 代码注释使用中文。
package duplicate

import (
	"path/filepath"

	"memable/internal/repo"
)

// ReportData 应用内重复报告数据。
type ReportData struct {
	Kind       string      `json:"kind"`
	GroupCount int         `json:"group_count"`
	FileCount  int         `json:"file_count"`
	Groups     []GroupView `json:"groups"`
}

// GroupView 分组视图。
type GroupView struct {
	Index  int        `json:"index"`
	Reason string     `json:"reason"`
	Items  []ItemView `json:"items"`
}

// ItemView 媒体条目视图。
type ItemView struct {
	ID             int64    `json:"id"`
	LibraryID      int64    `json:"library_id"`
	Kind           string   `json:"kind"`
	RelativePath   string   `json:"relative_path"`
	FullPath       string   `json:"full_path"`
	ThumbnailPath  string   `json:"thumbnail_path,omitempty"`
	Sha1           string   `json:"sha1,omitempty"`
	Width          int      `json:"width,omitempty"`
	Height         int      `json:"height,omitempty"`
	DurationMs     int64    `json:"duration_ms,omitempty"`
	FileSize       int64    `json:"file_size"`
	DuplicatePaths []string `json:"duplicate_paths"`
}

// BuildReport 将检测结果转换为可供 Flutter 展示的结构化报告。
func BuildReport(groups []Group, libraries []repo.Library, kind string) ReportData {
	libMap := make(map[int64]string)
	for _, l := range libraries {
		libMap[l.ID] = l.Path
	}

	views := buildGroupViews(groups, libMap)
	fileCount := 0
	for _, group := range views {
		fileCount += len(group.Items)
	}
	return ReportData{Kind: kind, GroupCount: len(views), FileCount: fileCount, Groups: views}
}

// buildGroupViews 将检测分组转为视图。
func buildGroupViews(groups []Group, libMap map[int64]string) []GroupView {
	views := make([]GroupView, 0, len(groups))
	for i, g := range groups {
		items := make([]ItemView, 0, len(g.Media))
		paths := make([]string, 0, len(g.Media))
		for _, m := range g.Media {
			paths = append(paths, filepath.Join(libMap[m.LibraryID], filepath.FromSlash(m.RelativePath)))
		}
		for _, m := range g.Media {
			item := ItemView{
				ID: m.ID, LibraryID: m.LibraryID, Kind: m.Kind, RelativePath: m.RelativePath,
				FullPath: filepath.Join(libMap[m.LibraryID], filepath.FromSlash(m.RelativePath)),
				FileSize: m.FileSize, DuplicatePaths: paths,
			}
			if m.Sha1 != nil {
				item.Sha1 = *m.Sha1
			}
			if m.Width != nil {
				item.Width = *m.Width
			}
			if m.Height != nil {
				item.Height = *m.Height
			}
			if m.DurationMs != nil {
				item.DurationMs = *m.DurationMs
			}
			if m.ThumbnailPath != nil {
				item.ThumbnailPath = *m.ThumbnailPath
			}
			items = append(items, item)
		}
		views = append(views, GroupView{
			Index:  i + 1,
			Reason: reasonLabel(g.Reason),
			Items:  items,
		})
	}
	return views
}

func reasonLabel(reason string) string {
	switch reason {
	case "sha1_exact":
		return "SHA1 完全相同"
	case "phash_similar":
		return "pHash 视觉相似"
	case "sprite_phash_similar":
		return "sprite pHash 视觉相似"
	case "oshash_short_exact":
		return "短视频 OSHash 相同"
	case "oshash_coarse":
		return "oshash 粗筛命中"
	default:
		return reason
	}
}
