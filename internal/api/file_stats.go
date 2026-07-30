// file_stats.go：文件统计核心逻辑（目录遍历、扩展名聚合、文件树构建）。
// 代码注释使用中文
package api

import (
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
