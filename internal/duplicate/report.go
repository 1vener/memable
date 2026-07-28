// report.go：重复检测 HTML 报告生成。
// 代码注释使用中文。
package duplicate

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"memable/internal/repo"
)

// ReportData HTML 报告模板数据。
type ReportData struct {
	Title     string
	Kind      string
	Groups    []GroupView
	Libraries map[int64]string // library_id → path，用于拼接完整路径
}

// GroupView 分组视图。
type GroupView struct {
	Index  int
	Reason string
	Items  []ItemView
}

// ItemView 媒体条目视图。
type ItemView struct {
	FullPath     string
	ThumbnailURL string // 缩略图相对于报告的路径
	Sha1         string
	Width        int
	Height       int
	DurationMs   int64
	FileSize     int64
}

// GenerateHTMLReport 生成 HTML 重复报告并保存到 outPath。
func GenerateHTMLReport(groups []Group, libraries []repo.Library, kind string, thumbBase string, outPath string) error {
	libMap := make(map[int64]string)
	for _, l := range libraries {
		libMap[l.ID] = l.Path
	}

	data := ReportData{
		Title:     fmt.Sprintf("memable %s 重复报告", kindLabel(kind)),
		Kind:      kind,
		Libraries: libMap,
		Groups:    buildGroupViews(groups, libMap, thumbBase),
	}

	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"formatSize":     formatSize,
		"formatDuration": formatDuration,
	}).Parse(reportTmpl)
	if err != nil {
		return fmt.Errorf("解析报告模板: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建报告文件 %q: %w", outPath, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("生成报告内容: %w", err)
	}
	return nil
}

// buildGroupViews 将检测分组转为视图。
func buildGroupViews(groups []Group, libMap map[int64]string, thumbBase string) []GroupView {
	views := make([]GroupView, 0, len(groups))
	for i, g := range groups {
		items := make([]ItemView, 0, len(g.Media))
		for _, m := range g.Media {
			item := ItemView{
				FullPath: filepath.Join(libMap[m.LibraryID], m.RelativePath),
				FileSize: m.FileSize,
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
				// 缩略图相对路径，HTML 中用 file:// 引用
				item.ThumbnailURL = filepath.Join(thumbBase, *m.ThumbnailPath)
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

func kindLabel(kind string) string {
	switch kind {
	case "image":
		return "图片"
	case "video":
		return "视频"
	default:
		return kind
	}
}

func reasonLabel(reason string) string {
	switch reason {
	case "sha1_exact":
		return "SHA1 完全相同"
	case "phash_similar":
		return "pHash 视觉相似"
	case "sprite_phash_similar":
		return "sprite pHash 视觉相似"
	case "oshash_coarse":
		return "oshash 粗筛命中"
	default:
		return reason
	}
}

const reportTmpl = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<style>
body { font-family: -apple-system, "Microsoft YaHei", sans-serif; margin: 20px; background: #f5f5f5; }
h1 { color: #333; }
.summary { background: #fff; padding: 12px 20px; border-radius: 8px; margin-bottom: 20px; }
.group { background: #fff; margin-bottom: 24px; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.group-header { background: #e8eaf6; padding: 10px 20px; font-weight: bold; color: #1a237e; }
.group-items { display: flex; flex-wrap: wrap; gap: 16px; padding: 16px; }
.item { border: 1px solid #ddd; border-radius: 6px; padding: 12px; width: 320px; }
.item img { max-width: 100%; border-radius: 4px; margin-bottom: 8px; }
.item .no-thumb { width: 100%; height: 200px; background: #eee; display: flex; align-items: center; justify-content: center; color: #999; border-radius: 4px; margin-bottom: 8px; }
.item .path { word-break: break-all; font-size: 12px; color: #555; }
.item .meta { font-size: 11px; color: #888; margin-top: 4px; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<div class="summary">
  <p>共发现 <strong>{{len .Groups}}</strong> 组重复/相似</p>
</div>

{{range .Groups}}
<div class="group">
  <div class="group-header">第 {{.Index}} 组 — {{.Reason}}（{{len .Items}} 个文件）</div>
  <div class="group-items">
    {{range .Items}}
    <div class="item">
      {{if .ThumbnailURL}}
        <img src="file:///{{.ThumbnailURL}}" alt="缩略图">
      {{else}}
        <div class="no-thumb">无缩略图</div>
      {{end}}
      <div class="path">{{.FullPath}}</div>
      <div class="meta">
        大小: {{formatSize .FileSize}}
        {{if .Width}}× {{.Width}}×{{.Height}}{{end}}
        {{if .DurationMs}}| 时长: {{formatDuration .DurationMs}}{{end}}
        <br>SHA1: {{.Sha1}}
      </div>
    </div>
    {{end}}
  </div>
</div>
{{end}}
</body>
</html>
`

// formatSize 将字节数格式化为人类可读字符串。
func formatSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	if b < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(b)/1024/1024)
	}
	return fmt.Sprintf("%.2f GB", float64(b)/1024/1024/1024)
}

// formatDuration 将毫秒格式化为 mm:ss。
func formatDuration(ms int64) string {
	sec := ms / 1000
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}
