// file_stats.go：文件统计核心逻辑（目录遍历、扩展名聚合、文件树构建、目录差异对比）。
// 代码注释使用中文
package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// extSummary 单个扩展名的统计摘要。
type extSummary struct {
	Ext      string  `json:"ext"`
	Bytes    int64   `json:"bytes"`
	Count    int64   `json:"count"`
	PctCount float64 `json:"pct_count"`
	PctBytes float64 `json:"pct_bytes"`
}

// walkResult 遍历结果（未包含百分比）。
type walkResult struct {
	totalBytes int64
	totalCount int64
	extStats   []extSummary
}

// fileTreeNodeRecursive 文件树节点（递归结构）。
type fileTreeNodeRecursive struct {
	Name     string                  `json:"name"`
	Path     string                  `json:"path"`
	IsDir    bool                    `json:"is_dir"`
	Ext      string                  `json:"ext,omitempty"`
	Size     int64                   `json:"size,omitempty"`
	Children []fileTreeNodeRecursive `json:"children,omitempty"`
}

// walkDirStats 递归遍历目录，计算总文件数、总大小、按扩展名统计和文件树。
func walkDirStats(rootPath string) (*walkResult, []fileTreeNodeRecursive) {
	extMap := make(map[string]*extSummary)
	var totalBytes int64
	var totalCount int64

	tree := buildFileTree(rootPath, "", extMap, &totalBytes, &totalCount)

	// 计算百分比并排序
	extList := make([]extSummary, 0, len(extMap))
	for _, v := range extMap {
		if totalCount > 0 {
			v.PctCount = float64(v.Count) * 100 / float64(totalCount)
		}
		if totalBytes > 0 {
			v.PctBytes = float64(v.Bytes) * 100 / float64(totalBytes)
		}
		extList = append(extList, *v)
	}
	sort.Slice(extList, func(i, j int) bool {
		return extList[i].Bytes > extList[j].Bytes
	})

	return &walkResult{
		totalBytes: totalBytes,
		totalCount: totalCount,
		extStats:   extList,
	}, tree
}

// buildFileTree 递归构建文件树，同时汇总扩展名字典和总量。
func buildFileTree(basePath, relPath string, extMap map[string]*extSummary, totalBytes, totalCount *int64) []fileTreeNodeRecursive {
	if extMap == nil {
		// 仅需要文件树（如 diff 对比）时允许不传扩展名统计
		extMap = map[string]*extSummary{}
	}
	absPath := filepath.Join(basePath, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil
	}

	nodes := make([]fileTreeNodeRecursive, 0, len(entries))
	for _, e := range entries {
		childRel := joinPath(relPath, e.Name())
		node := fileTreeNodeRecursive{
			Name:  e.Name(),
			Path:  childRel,
			IsDir: e.IsDir(),
		}
		if e.IsDir() {
			node.Children = buildFileTree(basePath, childRel, extMap, totalBytes, totalCount)
		} else {
			info, err := e.Info()
			if err == nil {
				node.Size = info.Size()
				*totalBytes += info.Size()
				*totalCount++
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if ext == "" {
					ext = "<无扩展名>"
				}
				node.Ext = ext
				s, ok := extMap[ext]
				if !ok {
					s = &extSummary{Ext: ext}
					extMap[ext] = s
				}
				s.Bytes += info.Size()
				s.Count++
			}
		}
		nodes = append(nodes, node)
	}
	// 目录排在文件前面，同类型按名称排序
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return nodes[i].Name < nodes[j].Name
	})
	return nodes
}

// fileDiff 目录差异对比结果。
type fileDiff struct {
	DirPath      string   `json:"dir_path"`      // 统计目录（绝对路径）
	Added        []string `json:"added"`         // 新增文件相对路径（正斜杠，字典序）
	Removed      []string `json:"removed"`       // 删除文件相对路径（正斜杠，字典序）
	AddedCount   int      `json:"added_count"`   // 新增文件数
	RemovedCount int      `json:"removed_count"` // 删除文件数
}

// computeFileDiff 对比历史统计与目录当前状态，返回新增/删除的文件相对路径。
// historyTreeJSON 为 file_stats.file_tree（文件树 JSON）；rootPath 为统计目录。
// 目录不存在时返回 (nil, err)，调用方据此提示用户。
func computeFileDiff(historyTreeJSON, rootPath string) (*fileDiff, error) {
	var history []fileTreeNodeRecursive
	if historyTreeJSON != "" {
		if err := json.Unmarshal([]byte(historyTreeJSON), &history); err != nil {
			return nil, err
		}
	}
	historySet := collectFilePaths(history)

	current := map[string]bool{}
	tree := buildFileTree(rootPath, "", nil, new(int64), new(int64))
	for p := range collectFilePaths(tree) {
		current[p] = true
	}

	var added, removed []string
	for p := range current {
		if !historySet[p] {
			added = append(added, p)
		}
	}
	for p := range historySet {
		if !current[p] {
			removed = append(removed, p)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return &fileDiff{
		DirPath:      rootPath,
		Added:        added,
		Removed:      removed,
		AddedCount:   len(added),
		RemovedCount: len(removed),
	}, nil
}

// collectFilePaths 从文件树递归收集全部文件（is_dir=false）的相对路径。
func collectFilePaths(nodes []fileTreeNodeRecursive) map[string]bool {
	out := map[string]bool{}
	var walk func(items []fileTreeNodeRecursive)
	walk = func(items []fileTreeNodeRecursive) {
		for _, n := range items {
			if n.IsDir {
				walk(n.Children)
			} else {
				out[n.Path] = true
			}
		}
	}
	walk(nodes)
	return out
}

// absolutePath 拼接统计目录与相对路径，返回正斜杠的绝对路径。
func absolutePath(rootPath, relPath string) string {
	return joinPath(rootPath, relPath)
}
