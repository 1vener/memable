// 包 search：媒体搜索服务（文本搜索 + 以图搜图 + 以视频搜视频）。
// 代码注释使用中文。
package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	Media    repo.Media `json:"media"`
	FullPath string     `json:"full_path"` // library.path + relative_path
	Distance int        `json:"distance"`  // 以图搜图时的 Hamming 距离（文本搜索为 0）
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

	// 合并去重（空切片必须初始化为非 nil，否则 JSON 序列化为 null，客户端强转 List 会崩）
	seen := make(map[int64]bool)
	results := make([]SearchResult, 0)
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

// SearchByImage 以图搜图：对比图片 pHash 与视频封面（缩略图）pHash。
// queryPHash 是查询图片的 pHash（16 位十六进制）。
// 注意：图片用 phash（全图，与缩略图内容等价）；视频不能用 sprite pHash
// （25 帧拼贴图，与单帧查询图不可比），必须用 cover_phash（封面帧 pHash）。
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

	results := make([]SearchResult, 0)
	for _, m := range all {
		var target *string
		if m.Kind == "video" {
			// 视频：对比封面帧（缩略图）pHash；v7 前存量未补齐 cover_phash 的跳过
			target = m.CoverPHash
		} else {
			target = m.Phash
		}
		if target == nil {
			continue
		}
		dist, err := media.HammingHex64(queryPHash, *target)
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

// SearchByVideo 以视频搜视频：提取查询视频的 sprite pHash 与首帧 pHash，
// 分别与库中视频 sprite pHash、库中图片 phash 对比（两个距离阈值独立可调）。
// 与视频重复检测不同：这里不区分时长差、不区分 <4000ms 短视频，统一按 sprite pHash 对比。
func (s *Service) SearchByVideo(ctx context.Context, videoPath string, imageMaxDistance, videoMaxDistance int) ([]SearchResult, error) {
	if videoMaxDistance <= 0 {
		videoMaxDistance = 16
	}
	if imageMaxDistance <= 0 {
		imageMaxDistance = 12
	}

	// 1. 元数据 + sprite pHash（与扫描同口径）
	meta, err := media.ProbeVideo(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("解析视频: %w", err)
	}
	spritePhash, err := media.ComputeVideoSpritePHash(ctx, videoPath, meta.DurationMs)
	if err != nil {
		return nil, fmt.Errorf("计算 sprite pHash: %w", err)
	}

	// 2. 首帧 pHash
	tmpDir, err := os.MkdirTemp("", "search-video-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	firstFramePath := filepath.Join(tmpDir, "first.jpg")
	if err := media.ExtractVideoFirstFrame(ctx, videoPath, firstFramePath); err != nil {
		return nil, fmt.Errorf("提取视频首帧: %w", err)
	}
	firstHashes, err := media.ImagePerceptualHashes(firstFramePath)
	if err != nil {
		return nil, fmt.Errorf("计算首帧 pHash: %w", err)
	}

	// 3. 图片按首帧 pHash 匹配（只对比图片，不对比视频 cover_phash）
	images, err := s.Media.ListByKind("image")
	if err != nil {
		return nil, fmt.Errorf("查询图片: %w", err)
	}
	results := make([]SearchResult, 0)
	for _, m := range images {
		if m.Phash == nil {
			continue
		}
		dist, err := media.HammingHex64(firstHashes.PHash, *m.Phash)
		if err != nil {
			continue
		}
		if dist <= imageMaxDistance {
			results = append(results, s.toResult(m, dist))
		}
	}

	// 4. 视频按 sprite pHash 匹配（不区分时长差/短视频）
	videos, err := s.Media.ListByKind("video")
	if err != nil {
		return nil, fmt.Errorf("查询视频: %w", err)
	}
	for _, m := range videos {
		if m.Phash == nil {
			continue
		}
		dist, err := media.HammingHex64(spritePhash, *m.Phash)
		if err != nil {
			continue
		}
		if dist <= videoMaxDistance {
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
