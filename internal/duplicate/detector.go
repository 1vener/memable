// 包 duplicate：图片/视频相似度与重复检测。
// 代码注释使用中文。
package duplicate

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"memable/internal/config"
	"memable/internal/media"
	"memable/internal/repo"
)

// Group 重复/相似分组。
type Group struct {
	Kind   string       // image/video
	Reason string       // sha1_exact / phash_similar / oshash_short_exact / oshash_coarse / sprite_phash_similar
	Media  []repo.Media // 组内媒体
}

// Detector 重复检测器。
type Detector struct {
	Media  *repo.MediaRepo
	Config *config.Config
}

// Options 重复报告生成选项。
type Options struct {
	Scope               string `json:"scope"`                  // all / same_dir（仅同一目录，不含子目录）
	MediaType           string `json:"media_type"`             // image / video / all
	ImageThreshold      int    `json:"image_threshold"`        // 图片相似度阈值 0-100（换算为 pHash Hamming 距离）
	VideoPhashDistance  int    `json:"video_phash_distance"`   // 视频 sprite pHash 最大 Hamming 距离
	VideoDurationDiffMs int64  `json:"video_duration_diff_ms"` // 视频允许时长差（毫秒）
	OshashFilter        bool   `json:"oshash_filter"`          // 是否启用 oshash 粗筛预分组
	IncludeSHA1         bool   `json:"include_sha1"`           // 是否包含 SHA1 完全相同结果
}

// NewDetector 创建检测器。
func NewDetector(mr *repo.MediaRepo, cfg *config.Config) *Detector {
	return &Detector{Media: mr, Config: cfg}
}

// DetectWithOptions 按生成选项检测图片/视频重复。
func (d *Detector) DetectWithOptions(opts Options) ([]Group, error) {
	var all []repo.Media
	var err error
	switch opts.MediaType {
	case "image", "video":
		all, err = d.Media.ListByKind(opts.MediaType)
	default:
		all, err = d.Media.ListAllFormal()
	}
	if err != nil {
		return nil, fmt.Errorf("查询媒体列表: %w", err)
	}
	slog.Info("重复检测开始", "media_type", opts.MediaType, "scope", opts.Scope, "count", len(all))

	imageMaxDist := distanceFromPercent(opts.ImageThreshold)
	var groups []Group
	if opts.Scope == "same_dir" {
		// 仅同一目录：按目录分桶后各自检测（不含子目录）
		buckets := map[string][]repo.Media{}
		for _, m := range all {
			key := relDir(m.RelativePath)
			buckets[key] = append(buckets[key], m)
		}
		for _, bucket := range buckets {
			groups = append(groups, d.detectBucket(bucket, opts, imageMaxDist)...)
		}
		groups = filterSameDir(groups)
	} else {
		groups = append(groups, d.detectBucket(all, opts, imageMaxDist)...)
	}
	slog.Info("重复检测完成", "groups", len(groups))
	return groups, nil
}

// detectBucket 对一组媒体执行图片/视频检测。
func (d *Detector) detectBucket(bucket []repo.Media, opts Options, imageMaxDist int) []Group {
	var groups []Group
	if opts.MediaType == "image" || opts.MediaType == "all" {
		groups = append(groups, d.detectImages(filterKind(bucket, "image"), opts, imageMaxDist)...)
	}
	if opts.MediaType == "video" || opts.MediaType == "all" {
		groups = append(groups, d.detectVideos(filterKind(bucket, "video"), opts)...)
	}
	return groups
}

func filterKind(items []repo.Media, kind string) []repo.Media {
	out := make([]repo.Media, 0, len(items))
	for _, m := range items {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

// distanceFromPercent 相似度百分比（0-100）换算为 64bit pHash Hamming 距离上限。
func distanceFromPercent(percent int) int {
	if percent <= 0 {
		return 64
	}
	if percent >= 100 {
		return 0
	}
	return 64 * (100 - percent) / 100
}

// detectImages 图片：SHA1 精确 + pHash 相似。
func (d *Detector) detectImages(images []repo.Media, opts Options, maxDist int) []Group {
	var groups []Group
	if opts.IncludeSHA1 {
		for _, items := range groupBySha1(images) {
			if len(items) >= 2 {
				groups = append(groups, Group{Kind: "image", Reason: "sha1_exact", Media: items})
			}
		}
	}
	represented := map[int64]bool{}
	for _, g := range groups {
		for _, m := range g.Media {
			represented[m.ID] = true
		}
	}
	candidates := make([]repo.Media, 0, len(images))
	for _, m := range images {
		if !represented[m.ID] && m.Phash != nil {
			candidates = append(candidates, m)
		}
	}
	groups = append(groups, d.detectImagePHashSimilarDist(candidates, maxDist)...)
	return groups
}

const shortVideoDurationMs int64 = 4000

// detectVideos 视频：短视频按 SHA1/OSHash 关系检测，普通视频按 sprite pHash/时长差检测。
func (d *Detector) detectVideos(videos []repo.Media, opts Options) []Group {
	groups, represented := d.detectVideoHashGroups(videos, opts.IncludeSHA1)

	// 短视频不参与 sprite pHash：即使指纹相近，也避免短时长视频抽帧信息不足造成误报。
	candidates := make([]repo.Media, 0, len(videos))
	for _, m := range videos {
		if represented[m.ID] || isShortVideo(m) {
			continue
		}
		if m.Phash != nil {
			candidates = append(candidates, m)
		}
	}
	groups = append(groups, d.detectVideoPHashSimilarDist(
		candidates, opts.VideoPhashDistance, opts.VideoDurationDiffMs, opts.OshashFilter)...)
	return groups
}

// detectVideoHashGroups 合并视频的 hash 关系。
// SHA1 关系适用于全部视频；OSHash 关系仅适用于短视频。
// 两种关系使用 OR 语义：任一 hash 相同即可连边，并查集负责合并传递关系。
func (d *Detector) detectVideoHashGroups(videos []repo.Media, includeSHA1 bool) ([]Group, map[int64]bool) {
	uf := newUnionFind(len(videos))
	indexByID := make(map[int64]int, len(videos))
	sha1Member := make([]bool, len(videos))
	for i, m := range videos {
		indexByID[m.ID] = i
	}

	if includeSHA1 {
		for _, items := range groupBySha1(videos) {
			if len(items) < 2 {
				continue
			}
			first, ok := indexByID[items[0].ID]
			if !ok {
				continue
			}
			for _, m := range items[1:] {
				if idx, ok := indexByID[m.ID]; ok {
					sha1Member[first] = true
					sha1Member[idx] = true
					uf.union(first, idx)
				}
			}
		}
	}

	shortVideos := make([]repo.Media, 0, len(videos))
	for _, m := range videos {
		if isShortVideo(m) {
			shortVideos = append(shortVideos, m)
		}
	}
	oshashGroups := groupByOshash(shortVideos)
	for _, items := range oshashGroups {
		if len(items) < 2 {
			continue
		}
		first, ok := indexByID[items[0].ID]
		if !ok {
			continue
		}
		for _, m := range items[1:] {
			if idx, ok := indexByID[m.ID]; ok {
				uf.union(first, idx)
			}
		}
	}

	components := make(map[int][]int)
	for i := range videos {
		root := uf.find(i)
		components[root] = append(components[root], i)
	}

	// 记录各连通分量的关系来源；若同时存在 SHA1 关系，优先保留 SHA1 精确标签。
	oshashComponent := make(map[int]bool)
	sha1Component := make(map[int]bool)
	for _, items := range oshashGroups {
		if len(items) < 2 {
			continue
		}
		if idx, ok := indexByID[items[0].ID]; ok {
			oshashComponent[uf.find(idx)] = true
		}
	}
	for idx, related := range sha1Member {
		if related {
			sha1Component[uf.find(idx)] = true
		}
	}

	groups := make([]Group, 0, len(components))
	represented := make(map[int64]bool)
	for root, indices := range components {
		if len(indices) < 2 {
			continue
		}
		reason := "sha1_exact"
		if !sha1Component[root] && oshashComponent[root] {
			reason = "oshash_short_exact"
		}
		mediaList := make([]repo.Media, 0, len(indices))
		for _, idx := range indices {
			mediaList = append(mediaList, videos[idx])
			represented[videos[idx].ID] = true
		}
		groups = append(groups, Group{Kind: "video", Reason: reason, Media: mediaList})
	}
	return groups, represented
}

// isShortVideo 判断是否为短视频；时长缺失时返回 false，沿用普通视频检测路径。
func isShortVideo(m repo.Media) bool {
	return m.DurationMs != nil && *m.DurationMs >= 0 && *m.DurationMs < shortVideoDurationMs
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
	return d.detectImagePHashSimilarDist(items, maxDist)
}

// detectImagePHashSimilarDist 用 pHash Hamming 距离检测相似图片。
func (d *Detector) detectImagePHashSimilarDist(items []repo.Media, maxDist int) []Group {
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

// DetectVideoDuplicates 检测视频重复：短视频按 SHA1/OSHash，普通视频按 sprite pHash + 时长差。
func (d *Detector) DetectVideoDuplicates() ([]Group, error) {
	videos, err := d.Media.ListByKind("video")
	if err != nil {
		return nil, fmt.Errorf("查询视频列表: %w", err)
	}
	slog.Info("视频重复检测开始", "count", len(videos))

	maxDist := 12
	maxDurationDiff := int64(3000)
	if d.Config != nil {
		if d.Config.Similarity.VideoPHashDistance > 0 {
			maxDist = d.Config.Similarity.VideoPHashDistance
		}
		if d.Config.Similarity.VideoDurationDiffMs > 0 {
			maxDurationDiff = d.Config.Similarity.VideoDurationDiffMs
		}
	}
	groups := d.detectVideos(videos, Options{
		MediaType:           "video",
		IncludeSHA1:         true,
		VideoPhashDistance:  maxDist,
		VideoDurationDiffMs: maxDurationDiff,
		OshashFilter:        true,
	})

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
	return d.detectVideoPHashSimilarDist(items, maxDist, maxDurationDiff, true)
}

// detectVideoPHashSimilarDist sprite pHash + 时长差检测；oshashFilter 开启时按 oshash 预分组加速。
func (d *Detector) detectVideoPHashSimilarDist(items []repo.Media, maxDist int, maxDurationDiff int64, oshashFilter bool) []Group {
	uf := newUnionFind(len(items))
	// oshash 只能作为粗筛提示，不能按不同值硬分桶，否则视觉相似但
	// oshash 不同的视频会被直接漏掉。当前实现统一进行 sprite pHash
	// 比较，保证召回完整性；参数保留用于报告选项兼容和后续安全优化。
	_ = oshashFilter
	idxs := make([]int, len(items))
	for i := range idxs {
		idxs[i] = i
	}
	unionVideoPairs(items, idxs, uf, maxDist, maxDurationDiff)

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

// unionVideoPairs 对一组索引做两两比较并合并满足条件的视频。
func unionVideoPairs(items []repo.Media, idxs []int, uf *unionFind, maxDist int, maxDurationDiff int64) {
	for i := 0; i < len(idxs); i++ {
		a := idxs[i]
		if items[a].Phash == nil {
			continue
		}
		for j := i + 1; j < len(idxs); j++ {
			b := idxs[j]
			if items[b].Phash == nil {
				continue
			}
			dist, err := media.HammingHex64(*items[a].Phash, *items[b].Phash)
			if err != nil || dist > maxDist {
				continue
			}
			durA := int64(0)
			if items[a].DurationMs != nil {
				durA = *items[a].DurationMs
			}
			durB := int64(0)
			if items[b].DurationMs != nil {
				durB = *items[b].DurationMs
			}
			diff := durA - durB
			if diff < 0 {
				diff = -diff
			}
			if diff <= maxDurationDiff {
				uf.union(a, b)
			}
		}
	}
}

// filterSameDir 仅保留全部成员位于同一目录（不含子目录）的重复组。
func filterSameDir(groups []Group) []Group {
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		dir := ""
		same := true
		for _, m := range g.Media {
			d := relDir(m.RelativePath)
			if dir == "" {
				dir = d
			} else if d != dir {
				same = false
				break
			}
		}
		if same && len(g.Media) >= 2 {
			out = append(out, g)
		}
	}
	return out
}

// relDir 返回相对路径的目录部分（正斜杠，根目录返回 "."）。
func relDir(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if dir == "." {
		return "."
	}
	return dir
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

// groupByOshash 按非空 OSHash 分组。
func groupByOshash(items []repo.Media) map[string][]repo.Media {
	groups := make(map[string][]repo.Media)
	for _, m := range items {
		if m.Oshash == nil || strings.TrimSpace(*m.Oshash) == "" {
			continue
		}
		groups[*m.Oshash] = append(groups[*m.Oshash], m)
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
