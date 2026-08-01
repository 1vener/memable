// 包 duplicate：图片/视频相似度与重复检测。
// 代码注释使用中文。
package duplicate

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

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
	// IncrementalSince 非零时启用增量检测：仅对 created_at >= 该时间的媒体
	// 发起查询（索引仍含全量媒体），用于重复报告生成加速。
	IncrementalSince *time.Time
	// Progress 可选进度回调（阶段名, total, processed, ...），用于报告生成任务进度展示。
	Progress repo.ProgressFunc
	// dirVsRest 目录对比模式：组必须同时包含目标与存量成员
	// （目标内部、存量内部不比较，避免目标内部相似文件自成一组）。
	dirVsRest bool
}

func (d *Detector) reportProgress(phase string, total, processed int) {
	if d.Progress != nil {
		d.Progress(phase, total, processed, processed, 0, 0, 0, 0, 0, (*int64)(nil))
	}
}

// Options 重复报告生成选项。
type Options struct {
	Scope               string `json:"scope"`                  // all / same_dir / dir_vs_rest
	MediaType           string `json:"media_type"`             // image / video / all
	ImageThreshold      int    `json:"image_threshold"`        // 图片相似度阈值 0-100（换算为 pHash Hamming 距离）
	VideoPhashDistance  int    `json:"video_phash_distance"`   // 视频 sprite pHash 最大 Hamming 距离
	VideoDurationDiffMs int64  `json:"video_duration_diff_ms"` // 视频允许时长差（毫秒）
	OshashFilter        bool   `json:"oshash_filter"`          // 是否启用 oshash 粗筛预分组
	IncludeSHA1         bool   `json:"include_sha1"`           // 是否包含 SHA1 完全相同结果
	// dir_vs_rest 模式：所选目录（含子目录）与其余存量数据对比；
	// 目标内部、存量内部均不比较，只产生"目标 vs 存量"的重复组。
	LibraryID int64  `json:"library_id,omitempty"` // 所选目录所属收藏库
	Directory string `json:"directory,omitempty"`  // 所选目录相对库根路径（正斜杠，含子目录）
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

	// 查询目标掩码：dir_vs_rest 模式 = 所选目录（含子目录）的媒体；
	// 增量模式 = created_at >= IncrementalSince 的媒体。
	// 索引始终含全量媒体，保证目标文件能匹配到存量文件；存量内部、目标内部均不比较。
	var queryMask []bool
	if opts.Scope == "dir_vs_rest" {
		d.dirVsRest = true
		queryMask = make([]bool, len(all))
		for i, m := range all {
			if m.LibraryID == opts.LibraryID && isUnderDir(m.RelativePath, opts.Directory) {
				queryMask[i] = true
			}
		}
	} else if d.IncrementalSince != nil && len(all) > 0 {
		queryMask = make([]bool, len(all))
		for i, m := range all {
			if !m.CreatedAt.Before(*d.IncrementalSince) {
				queryMask[i] = true
			}
		}
	}

	slog.Info("重复检测开始", "media_type", opts.MediaType, "scope", opts.Scope, "count", len(all),
		"incremental", queryMask != nil && opts.Scope != "dir_vs_rest")
	d.reportProgress("loading", len(all), len(all))

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
			groups = append(groups, d.detectBucket(bucket, opts, imageMaxDist, queryMaskForBucket(queryMask, all, bucket))...)
		}
		groups = filterSameDir(groups)
	} else {
		groups = append(groups, d.detectBucket(all, opts, imageMaxDist, queryMask)...)
	}
	slog.Info("重复检测完成", "groups", len(groups))
	return groups, nil
}

// queryMaskForBucket 从全量 queryMask 中提取对应桶的掩码（增量检测按目录分桶时用）。
func queryMaskForBucket(fullMask []bool, all, bucket []repo.Media) []bool {
	if fullMask == nil {
		return nil
	}
	byID := make(map[int64]int, len(all))
	for i, m := range all {
		byID[m.ID] = i
	}
	mask := make([]bool, len(bucket))
	for i, m := range bucket {
		if idx, ok := byID[m.ID]; ok && fullMask[idx] {
			mask[i] = true
		}
	}
	return mask
}

// detectBucket 对一组媒体执行图片/视频检测。
// queryMask 与 bucket 等长，true=增量检测的查询目标（nil=全量查询）。
func (d *Detector) detectBucket(bucket []repo.Media, opts Options, imageMaxDist int, queryMask []bool) []Group {
	var groups []Group
	if opts.MediaType == "image" || opts.MediaType == "all" {
		images, mask := filterKindWithMask(bucket, "image", queryMask)
		groups = append(groups, d.detectImages(images, mask, opts, imageMaxDist)...)
	}
	if opts.MediaType == "video" || opts.MediaType == "all" {
		videos, mask := filterKindWithMask(bucket, "video", queryMask)
		groups = append(groups, d.detectVideos(videos, mask, opts)...)
	}
	return groups
}

// filterKindWithMask 过滤指定类型，同时裁剪增量查询掩码（与输出等长，nil=全量）。
func filterKindWithMask(items []repo.Media, kind string, queryMask []bool) ([]repo.Media, []bool) {
	out := make([]repo.Media, 0, len(items))
	if queryMask == nil {
		for _, m := range items {
			if m.Kind == kind {
				out = append(out, m)
			}
		}
		return out, nil
	}
	var outMask []bool
	for i, m := range items {
		if m.Kind == kind {
			out = append(out, m)
			outMask = append(outMask, queryMask[i])
		}
	}
	return out, outMask
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
// queryMask 与 images 等长，true=增量查询目标（nil=全量）。
func (d *Detector) detectImages(images []repo.Media, queryMask []bool, opts Options, maxDist int) []Group {
	// 增量模式：记录新文件 ID，SHA1 组只处理包含新文件的组
	isNew := newIDsFromMask(images, queryMask)

	var groups []Group
	if opts.IncludeSHA1 {
		for _, items := range groupBySha1(images) {
			if len(items) >= 2 && d.keepHashGroup(items, isNew) {
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
	var candidateMask []bool
	for i, m := range images {
		if !represented[m.ID] && m.Phash != nil {
			candidates = append(candidates, m)
			if queryMask != nil {
				candidateMask = append(candidateMask, queryMask[i])
			}
		}
	}
	groups = append(groups, d.detectImagePHashSimilarDist(candidates, maxDist, candidateMask)...)
	return groups
}

// keepHashGroup 判断 SHA1/OSHash 组是否应保留：
// 增量模式：组内含新媒体即可；目录对比模式：组内必须同时包含目标与存量成员。
func (d *Detector) keepHashGroup(items []repo.Media, isNew map[int64]bool) bool {
	if isNew == nil {
		return true
	}
	if !groupHasNew(items, isNew) {
		return false
	}
	if d.dirVsRest {
		// 目录对比：目标内部不比较，组内必须存在存量成员
		for _, m := range items {
			if !isNew[m.ID] {
				return true
			}
		}
		return false
	}
	return true
}

// newIDsFromMask 从查询掩码构建"新文件 ID"集合（queryMask=nil 时返回 nil）。
func newIDsFromMask(items []repo.Media, queryMask []bool) map[int64]bool {
	if queryMask == nil {
		return nil
	}
	out := make(map[int64]bool)
	for i, m := range items {
		if queryMask[i] {
			out[m.ID] = true
		}
	}
	return out
}

// groupHasNew 判断组内是否包含新文件（isNew=nil 时恒为 true）。
func groupHasNew(items []repo.Media, isNew map[int64]bool) bool {
	if isNew == nil {
		return true
	}
	for _, m := range items {
		if isNew[m.ID] {
			return true
		}
	}
	return false
}

const shortVideoDurationMs int64 = 4000

// detectVideos 视频：短视频按 SHA1/OSHash 关系检测，普通视频按 sprite pHash/时长差检测。
// queryMask 与 videos 等长，true=增量查询目标（nil=全量）。
func (d *Detector) detectVideos(videos []repo.Media, queryMask []bool, opts Options) []Group {
	groups, represented := d.detectVideoHashGroups(videos, opts.IncludeSHA1, queryMask)

	// 短视频不参与 sprite pHash：即使指纹相近，也避免短时长视频抽帧信息不足造成误报。
	candidates := make([]repo.Media, 0, len(videos))
	var candidateMask []bool
	for i, m := range videos {
		if represented[m.ID] || isShortVideo(m) {
			continue
		}
		if m.Phash != nil {
			candidates = append(candidates, m)
			if queryMask != nil {
				candidateMask = append(candidateMask, queryMask[i])
			}
		}
	}
	groups = append(groups, d.detectVideoPHashSimilarDist(
		candidates, opts.VideoPhashDistance, opts.VideoDurationDiffMs, opts.OshashFilter, candidateMask)...)
	return groups
}

// detectVideoHashGroups 合并视频的 hash 关系。
// SHA1 关系适用于全部视频；OSHash 关系仅适用于短视频。
// 两种关系使用 OR 语义：任一 hash 相同即可连边，并查集负责合并传递关系。
// queryMask 与 videos 等长，true=增量查询目标（nil=全量）。
func (d *Detector) detectVideoHashGroups(videos []repo.Media, includeSHA1 bool, queryMask []bool) ([]Group, map[int64]bool) {
	uf := newUnionFind(len(videos))
	indexByID := make(map[int64]int, len(videos))
	sha1Member := make([]bool, len(videos))
	for i, m := range videos {
		indexByID[m.ID] = i
	}
	isNew := newIDsFromMask(videos, queryMask)

	if includeSHA1 {
		for _, items := range groupBySha1(videos) {
			if len(items) < 2 || !d.keepHashGroup(items, isNew) {
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
		if len(items) < 2 || !d.keepHashGroup(items, isNew) {
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
	return d.detectImagePHashSimilarDist(items, maxDist, nil)
}

// detectImagePHashSimilarDist 用 pHash Hamming 距离检测相似图片（MIH 加速）。
// maxDist=0 表示只匹配完全相同的哈希。
// queryMask 与 items 等长，true=增量查询目标（nil=全量查询）。
func (d *Detector) detectImagePHashSimilarDist(items []repo.Media, maxDist int, queryMask []bool) []Group {
	if len(items) == 0 || maxDist < 0 {
		return nil
	}
	uf := newUnionFind(len(items))

	// 构建 MIH 索引，查询候选对（增量模式只对 queryMask 为 true 的项查询）
	idx := NewPhashIndex(items)
	queryIdx := maskIndices(queryMask)
	candidates := idx.QueryAll(maxDist, queryIdx)
	for i, cands := range candidates {
		if i%1000 == 0 {
			d.reportProgress("phash", len(items), i)
		}
		for _, j := range cands {
			if media.HammingUint64(idx.hashes[i], idx.hashes[j]) <= maxDist {
				uf.union(i, j)
			}
		}
	}
	d.reportProgress("phash", len(items), len(items))

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
	groups := d.detectVideos(videos, nil, Options{
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
	return d.detectVideoPHashSimilarDist(items, maxDist, maxDurationDiff, true, nil)
}

// detectVideoPHashSimilarDist sprite pHash + 时长差检测（MIH 加速 + 时长分桶）。
// maxDist=0 表示只匹配完全相同的哈希。
// queryMask 与 items 等长，true=增量查询目标（nil=全量查询）。
func (d *Detector) detectVideoPHashSimilarDist(items []repo.Media, maxDist int, maxDurationDiff int64, oshashFilter bool, queryMask []bool) []Group {
	if len(items) == 0 || maxDist < 0 {
		return nil
	}
	// oshash 只能作为粗筛提示，不能按不同值硬分桶，否则视觉相似但
	// oshash 不同的视频会被直接漏掉。当前实现统一进行 sprite pHash
	// 比较，保证召回完整性；参数保留用于报告选项兼容和后续安全优化。
	_ = oshashFilter

	uf := newUnionFind(len(items))

	// 按时长分桶：只有时长差 ≤ maxDurationDiff 的视频才需要比较。
	// 桶宽 = maxDurationDiff，比较同桶与相邻桶。
	if maxDurationDiff > 0 {
		buckets := bucketByDuration(items, maxDurationDiff)
		for _, bucket := range buckets {
			if len(bucket) < 2 {
				continue
			}
			unionVideoMIH(items, bucket, uf, maxDist, maxDurationDiff, queryMask)
		}
	} else {
		// 时长差为 0 时仍需全量比较（不按时长分桶）
		idxs := make([]int, len(items))
		for i := range idxs {
			idxs[i] = i
		}
		unionVideoMIH(items, idxs, uf, maxDist, 0, queryMask)
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

// bucketByDuration 按 duration_ms 分桶（桶宽 = maxDurationDiff），返回每桶的全局索引。
// 只有同桶或相邻桶的视频才可能满足时长差约束。
func bucketByDuration(items []repo.Media, maxDurationDiff int64) [][]int {
	bucketMap := make(map[int64][]int)
	for i, m := range items {
		dur := int64(0)
		if m.DurationMs != nil {
			dur = *m.DurationMs
		}
		bucketKey := dur / maxDurationDiff
		bucketMap[bucketKey] = append(bucketMap[bucketKey], i)
	}

	// 合并相邻桶（时长差可能跨桶边界）
	seen := make(map[int64]bool)
	var buckets [][]int
	for key, indices := range bucketMap {
		if seen[key] {
			continue
		}
		merged := append([]int(nil), indices...)
		seen[key] = true
		// 合并相邻桶 key-1, key+1
		for _, adj := range []int64{key - 1, key + 1} {
			if !seen[adj] {
				if adjIndices, ok := bucketMap[adj]; ok {
					merged = append(merged, adjIndices...)
					seen[adj] = true
				}
			}
		}
		if len(merged) >= 2 {
			buckets = append(buckets, merged)
		}
	}
	return buckets
}

// unionVideoMIH 对一组视频索引使用 MIH 查询候选，验证 pHash 距离 + 时长差后合并。
// queryMask 与 items 等长，true=增量查询目标（nil=全量）。
func unionVideoMIH(items []repo.Media, idxs []int, uf *unionFind, maxDist int, maxDurationDiff int64, queryMask []bool) {
	if len(idxs) < 2 {
		return
	}
	// 构建子集的 MIH 索引；增量模式只对 queryMask 为 true 的项发起查询
	subset := make([]repo.Media, len(idxs))
	subsetMask := make([]bool, len(idxs))
	for i, idx := range idxs {
		subset[i] = items[idx]
		subsetMask[i] = queryMask == nil || queryMask[idx]
	}
	if queryMask == nil {
		subsetMask = nil
	}
	idx := NewPhashIndex(subset)
	candidates := idx.QueryAll(maxDist, maskIndices(subsetMask))
	for i, cands := range candidates {
		for _, j := range cands {
			if media.HammingUint64(idx.hashes[i], idx.hashes[j]) > maxDist {
				continue
			}
			if maxDurationDiff > 0 {
				aDur := int64(0)
				if items[idxs[i]].DurationMs != nil {
					aDur = *items[idxs[i]].DurationMs
				}
				bDur := int64(0)
				if items[idxs[j]].DurationMs != nil {
					bDur = *items[idxs[j]].DurationMs
				}
				diff := aDur - bDur
				if diff < 0 {
					diff = -diff
				}
				if diff > maxDurationDiff {
					continue
				}
			}
			uf.union(idxs[i], idxs[j])
		}
	}
}

// maskIndices 将布尔掩码转换为要查询的索引列表（nil=全部）。
func maskIndices(mask []bool) []int {
	if mask == nil {
		return nil
	}
	out := make([]int, 0)
	for i, v := range mask {
		if v {
			out = append(out, i)
		}
	}
	return out
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

// isUnderDir 判断相对路径是否位于指定目录内（含子目录）。
// dir 为空或 "." 表示库根目录（全部匹配）。
func isUnderDir(rel, dir string) bool {
	rel = strings.ReplaceAll(rel, "\\", "/")
	dir = strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	if dir == "" || dir == "." {
		return true
	}
	return rel == dir || strings.HasPrefix(rel, dir+"/")
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
