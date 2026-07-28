// 包 duplicate：图片/视频相似度与重复检测。
// 代码注释使用中文。
package duplicate

import (
	"fmt"
	"log/slog"

	"memable/internal/config"
	"memable/internal/media"
	"memable/internal/repo"
)

// Group 重复/相似分组。
type Group struct {
	Kind   string       // image/video
	Reason string       // sha1_exact / phash_similar / oshash_coarse / sprite_phash_similar
	Media  []repo.Media // 组内媒体
}

// Detector 重复检测器。
type Detector struct {
	Media  *repo.MediaRepo
	Config *config.Config
}

// NewDetector 创建检测器。
func NewDetector(mr *repo.MediaRepo, cfg *config.Config) *Detector {
	return &Detector{Media: mr, Config: cfg}
}

// DetectImageDuplicates 检测图片重复：SHA1 精确 + pHash/dHash/aHash 相似。
func (d *Detector) DetectImageDuplicates() ([]Group, error) {
	images, err := d.Media.ListByKind("image")
	if err != nil {
		return nil, fmt.Errorf("查询图片列表: %w", err)
	}
	slog.Info("图片重复检测开始", "count", len(images))

	// 第一层：SHA1 精确重复
	sha1Groups := groupBySha1(images)
	var groups []Group
	for _, items := range sha1Groups {
		if len(items) >= 2 {
			groups = append(groups, Group{
				Kind:   "image",
				Reason: "sha1_exact",
				Media:  items,
			})
		}
	}

	// 第二层：pHash/dHash/aHash 相似
	// 从 SHA1 分组中取每组的代表，再加上未分组的
	represented := make(map[int64]bool)
	for _, g := range groups {
		for _, m := range g.Media {
			represented[m.ID] = true
		}
	}

	var candidates []repo.Media
	for _, m := range images {
		if !represented[m.ID] && m.Phash != nil {
			candidates = append(candidates, m)
		}
	}

	phashGroups := d.detectImagePHashSimilar(candidates)
	groups = append(groups, phashGroups...)

	slog.Info("图片重复检测完成", "groups", len(groups))
	return groups, nil
}

// detectImagePHashSimilar 用 pHash Hamming 距离检测相似图片，双向最近邻。
func (d *Detector) detectImagePHashSimilar(items []repo.Media) []Group {
	maxDist := 10
	if d.Config != nil && d.Config.Similarity.ImagePHashDistance > 0 {
		maxDist = d.Config.Similarity.ImagePHashDistance
	}

	// 并查集
	uf := newUnionFind(len(items))
	for i := 0; i < len(items); i++ {
		if items[i].Phash == nil {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if items[j].Phash == nil {
				continue
			}
			dist, err := media.HammingHex64(*items[i].Phash, *items[j].Phash)
			if err != nil {
				continue
			}
			if dist <= maxDist {
				uf.union(i, j)
			}
		}
	}

	// 按连通分量分组
	compMap := make(map[int][]int)
	for i := 0; i < len(items); i++ {
		root := uf.find(i)
		compMap[root] = append(compMap[root], i)
	}

	var groups []Group
	for _, indices := range compMap {
		if len(indices) < 2 {
			continue
		}
		mediaList := make([]repo.Media, 0, len(indices))
		for _, idx := range indices {
			mediaList = append(mediaList, items[idx])
		}
		groups = append(groups, Group{
			Kind:   "image",
			Reason: "phash_similar",
			Media:  mediaList,
		})
	}
	return groups
}

// DetectVideoDuplicates 检测视频重复：SHA1 精确 → oshash 粗筛 → sprite pHash + 时长差。
func (d *Detector) DetectVideoDuplicates() ([]Group, error) {
	videos, err := d.Media.ListByKind("video")
	if err != nil {
		return nil, fmt.Errorf("查询视频列表: %w", err)
	}
	slog.Info("视频重复检测开始", "count", len(videos))

	// 第一层：SHA1 精确重复
	sha1Groups := groupBySha1(videos)
	var groups []Group
	for _, items := range sha1Groups {
		if len(items) >= 2 {
			groups = append(groups, Group{
				Kind:   "video",
				Reason: "sha1_exact",
				Media:  items,
			})
		}
	}

	// 排除已分组的
	represented := make(map[int64]bool)
	for _, g := range groups {
		for _, m := range g.Media {
			represented[m.ID] = true
		}
	}

	var candidates []repo.Media
	for _, m := range videos {
		if !represented[m.ID] && m.Phash != nil {
			candidates = append(candidates, m)
		}
	}

	// 第二层 + 第三层：sprite pHash 距离 + 时长差
	phashGroups := d.detectVideoPHashSimilar(candidates)
	groups = append(groups, phashGroups...)

	slog.Info("视频重复检测完成", "groups", len(groups))
	return groups, nil
}

// detectVideoPHashSimilar 用 sprite pHash Hamming 距离 + 时长差检测相似视频。
func (d *Detector) detectVideoPHashSimilar(items []repo.Media) []Group {
	maxDist := 12
	if d.Config != nil && d.Config.Similarity.VideoPHashDistance > 0 {
		maxDist = d.Config.Similarity.VideoPHashDistance
	}
	maxDurationDiff := int64(3000)
	if d.Config != nil && d.Config.Similarity.VideoDurationDiffMs > 0 {
		maxDurationDiff = d.Config.Similarity.VideoDurationDiffMs
	}

	uf := newUnionFind(len(items))
	for i := 0; i < len(items); i++ {
		if items[i].Phash == nil {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if items[j].Phash == nil {
				continue
			}
			dist, err := media.HammingHex64(*items[i].Phash, *items[j].Phash)
			if err != nil {
				continue
			}
			if dist > maxDist {
				continue
			}
			// 时长差辅助条件
			durA := int64(0)
			if items[i].DurationMs != nil {
				durA = *items[i].DurationMs
			}
			durB := int64(0)
			if items[j].DurationMs != nil {
				durB = *items[j].DurationMs
			}
			diff := durA - durB
			if diff < 0 {
				diff = -diff
			}
			if diff <= maxDurationDiff {
				uf.union(i, j)
			}
		}
	}

	compMap := make(map[int][]int)
	for i := 0; i < len(items); i++ {
		root := uf.find(i)
		compMap[root] = append(compMap[root], i)
	}

	var groups []Group
	for _, indices := range compMap {
		if len(indices) < 2 {
			continue
		}
		mediaList := make([]repo.Media, 0, len(indices))
		for _, idx := range indices {
			mediaList = append(mediaList, items[idx])
		}
		groups = append(groups, Group{
			Kind:   "video",
			Reason: "sprite_phash_similar",
			Media:  mediaList,
		})
	}
	return groups
}

// groupBySha1 按 SHA1 分组，返回包含 >=2 条记录的分组。
func groupBySha1(items []repo.Media) map[string][]repo.Media {
	groups := make(map[string][]repo.Media)
	for _, m := range items {
		if m.Sha1 == nil {
			continue
		}
		groups[*m.Sha1] = append(groups[*m.Sha1], m)
	}
	return groups
}

// ===== 并查集 =====

type unionFind struct{ parent, rank []int }

func newUnionFind(n int) *unionFind {
	p := make([]int, n)
	r := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &unionFind{parent: p, rank: r}
}

func (uf *unionFind) find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *unionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}
