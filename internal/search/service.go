// 包 search：媒体搜索服务（文本搜索 + 以图搜图）。
// 代码注释使用中文。
package search

import (
	"fmt"
	"sort"

	"memable/internal/media"
	"memable/internal/repo"
)

// Service 搜索服务。
type Service struct {
	Media     *repo.MediaRepo
	Libraries *repo.LibraryRepo
}

// SearchResult 搜索结果条目。
type SearchResult struct {
	Media    repo.Media
	FullPath string // library.path + relative_path
	Distance int    // 以图搜图时的 Hamming 距离（文本搜索为 0）
}

// NewService 创建搜索服务。
func NewService(mr *repo.MediaRepo, lr *repo.LibraryRepo) *Service {
	return &Service{Media: mr, Libraries: lr}
}

// SearchByText 文本搜索：文件名模糊匹配全路径 + SHA1 精确。
func (s *Service) SearchByText(query string) ([]SearchResult, error) {
	query = trimSpace(query)
	if query == "" {
		return nil, nil
	}

	// 先尝试 SHA1 精确搜索
	sha1Results, err := s.Media.SearchBySha1(query)
	if err != nil {
		return nil, fmt.Errorf("SHA1 搜索: %w", err)
	}

	// 路径模糊搜索
	pathResults, err := s.Media.SearchByPath(query)
	if err != nil {
		return nil, fmt.Errorf("路径搜索: %w", err)
	}

	// 合并去重
	seen := make(map[int64]bool)
	var results []SearchResult
	for _, m := range sha1Results {
		if !seen[m.ID] {
			results = append(results, s.toResult(m, 0))
			seen[m.ID] = true
		}
	}
	for _, m := range pathResults {
		if !seen[m.ID] {
			results = append(results, s.toResult(m, 0))
			seen[m.ID] = true
		}
	}
	return results, nil
}

// SearchByImage 以图搜图：对比图片 pHash 与视频 sprite pHash。
// queryPHash 是查询图片的 pHash（16 位十六进制）。
func (s *Service) SearchByImage(queryPHash string, maxDistance int) ([]SearchResult, error) {
	if maxDistance <= 0 {
		maxDistance = 12
	}

	all, err := s.Media.ListByKind("image")
	if err != nil {
		return nil, fmt.Errorf("查询图片: %w", err)
	}
	videos, err := s.Media.ListByKind("video")
	if err != nil {
		return nil, fmt.Errorf("查询视频: %w", err)
	}
	all = append(all, videos...)

	var results []SearchResult
	for _, m := range all {
		if m.Phash == nil {
			continue
		}
		dist, err := media.HammingHex64(queryPHash, *m.Phash)
		if err != nil {
			continue
		}
		if dist <= maxDistance {
			results = append(results, s.toResult(m, dist))
		}
	}

	// 按距离升序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Distance < results[j].Distance
	})
	return results, nil
}

// toResult 将 Media 转为 SearchResult，填充完整路径。
func (s *Service) toResult(m repo.Media, distance int) SearchResult {
	lib, err := s.Libraries.GetByID(m.LibraryID)
	fullPath := m.RelativePath
	if err == nil {
		fullPath = lib.Path + "/" + m.RelativePath
	}
	return SearchResult{
		Media:    m,
		FullPath: fullPath,
		Distance: distance,
	}
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}
