// mih.go：Multi-Index Hashing（MIH）索引，用于 pHash 近邻快速查询。
// 将 64-bit pHash 拆成 4 段 × 16 bit，利用鸽笼原理：
// 若两哈希 Hamming 距离 ≤ d，则 4 段中必有一段距离 ≤ ⌊d/4⌋。
// 查询时只需枚举每段的 ⌊d/4⌋ 邻居桶，收集候选后再精确校验。
// 复杂度从 O(n²) 降至 O(n·k + 候选数)，理论上无漏检。
// 代码注释使用中文。
package duplicate

import (
	"log/slog"

	"memable/internal/media"
	"memable/internal/repo"
)

const (
	mihNumSegments = 4                      // 64 bit 拆成 4 段
	mihSegWidth    = 64 / mihNumSegments    // 每段 16 bit
	mihSegMask     = (1 << mihSegWidth) - 1 // 0xFFFF
	mihNumBuckets  = 1 << mihSegWidth       // 每段 65536 个桶
)

// PhashIndex Multi-Index Hashing 索引。
type PhashIndex struct {
	hashes   []uint64 // 预解析 pHash
	mediaIDs []int64  // 对应 media ID
	// 每段桶的 postings 偏移和全局 postings 数组（紧凑存储）
	bucketOffset [][]uint32 // [numSegments][numBuckets] 桶起始偏移
	postings     []uint32   // 全局 postings，桶内连续
}

// NewPhashIndex 创建 MIH 索引。
func NewPhashIndex(items []repo.Media) *PhashIndex {
	idx := &PhashIndex{}
	idx.build(items)
	return idx
}

// build 解析 pHash 并构建 4 段 × 65536 桶的 postings 索引。
func (idx *PhashIndex) build(items []repo.Media) {
	// 第一遍：解析 pHash，统计每桶条目数
	idx.hashes = make([]uint64, 0, len(items))
	idx.mediaIDs = make([]int64, 0, len(items))
	counts := make([][]uint32, mihNumSegments)
	for seg := 0; seg < mihNumSegments; seg++ {
		counts[seg] = make([]uint32, mihNumBuckets)
	}

	for _, m := range items {
		if m.Phash == nil {
			continue
		}
		h, err := media.ParseHex64(*m.Phash)
		if err != nil {
			continue
		}
		idx.hashes = append(idx.hashes, h)
		idx.mediaIDs = append(idx.mediaIDs, m.ID)
		for seg := 0; seg < mihNumSegments; seg++ {
			val := segValue(h, seg)
			counts[seg][val]++
		}
	}

	n := len(idx.hashes)
	if n == 0 {
		return
	}

	// 计算每段桶的起始偏移（前缀和）
	idx.bucketOffset = make([][]uint32, mihNumSegments)
	totalPosts := uint32(0)
	for seg := 0; seg < mihNumSegments; seg++ {
		idx.bucketOffset[seg] = make([]uint32, mihNumBuckets+1)
		for b := 0; b < mihNumBuckets; b++ {
			idx.bucketOffset[seg][b] = totalPosts
			totalPosts += counts[seg][b]
		}
		idx.bucketOffset[seg][mihNumBuckets] = totalPosts
	}

	// 第二遍：填充 postings（每段独立计数）
	idx.postings = make([]uint32, totalPosts)
	for seg := 0; seg < mihNumSegments; seg++ {
		offsets := make([]uint32, mihNumBuckets)
		for i := 0; i < n; i++ {
			val := segValue(idx.hashes[i], seg)
			pos := idx.bucketOffset[seg][val] + offsets[val]
			idx.postings[pos] = uint32(i)
			offsets[val]++
		}
	}

	slog.Info("MIH 索引构建完成", "items", n, "postings", totalPosts)
}

// Query 查询与 hash 距离 ≤ maxDist 的候选索引（返回全局索引，不含自身）。
// maxDist=0 表示只匹配完全相同的哈希。
func (idx *PhashIndex) Query(hash uint64, maxDist int) []int {
	if len(idx.hashes) == 0 || maxDist < 0 {
		return nil
	}
	t := maxDist / mihNumSegments // 每段允许距离
	if t > mihSegWidth {
		t = mihSegWidth
	}

	seen := make(map[uint32]bool)
	var candidates []int

	for seg := 0; seg < mihNumSegments; seg++ {
		val := segValue(hash, seg)
		for _, bv := range enumerateBuckets(val, t) {
			start := idx.bucketOffset[seg][bv]
			end := idx.bucketOffset[seg][bv+1]
			for p := start; p < end; p++ {
				ci := idx.postings[p]
				if !seen[ci] {
					seen[ci] = true
					candidates = append(candidates, int(ci))
				}
			}
		}
	}
	return candidates
}

// QueryAll 批量查询候选对。
// 返回每个项目 i 的候选索引列表（仅包含 j > i 的有效候选）。
// queryIdx 指定只对哪些索引发起查询（nil=全部查询）；用于增量检测。
// maxDist=0 表示只匹配完全相同的哈希（距离为 0 的对）。
func (idx *PhashIndex) QueryAll(maxDist int, queryIdx []int) [][]int {
	n := len(idx.hashes)
	if n == 0 || maxDist < 0 {
		return nil
	}
	t := maxDist / mihNumSegments
	if t > mihSegWidth {
		t = mihSegWidth
	}

	results := make([][]int, n)
	seen := make(map[uint64]bool, n*2)

	// 确定要查询的索引范围
	iterate := func(f func(i int)) {
		if queryIdx == nil {
			for i := 0; i < n; i++ {
				f(i)
			}
			return
		}
		for _, i := range queryIdx {
			if i >= 0 && i < n {
				f(i)
			}
		}
	}

	iterate(func(i int) {
		hash := idx.hashes[i]
		for seg := 0; seg < mihNumSegments; seg++ {
			val := segValue(hash, seg)
			for _, bv := range enumerateBuckets(val, t) {
				start := idx.bucketOffset[seg][bv]
				end := idx.bucketOffset[seg][bv+1]
				for p := start; p < end; p++ {
					ci := int(idx.postings[p])
					if ci <= i {
						continue
					}
					pairKey := uint64(i)<<32 | uint64(ci)
					if seen[pairKey] {
						continue
					}
					seen[pairKey] = true
					results[i] = append(results[i], ci)
				}
			}
		}
	})
	return results
}

// segValue 提取 hash 第 seg 段的值（低段在右）。
func segValue(hash uint64, seg int) uint32 {
	return uint32((hash >> uint(seg*mihSegWidth)) & mihSegMask)
}

// enumerateBuckets 枚举段值 val 所有距离 ≤ t 的邻居值（含 val 自身）。
func enumerateBuckets(val uint32, t int) []uint32 {
	// 估计容量：1 + C(16,1) + C(16,2) + ... + C(16,t)
	cap := 1
	for k := 1; k <= t; k++ {
		cap += comb(mihSegWidth, k)
	}
	buckets := make([]uint32, 0, cap)
	buckets = append(buckets, val)

	// 逐步翻转 bit 的组合
	// 使用递归枚举所有 ≤ t 位翻转
	var flipBits func(start, remaining int, current uint32)
	flipBits = func(start, remaining int, current uint32) {
		if remaining == 0 {
			if current != val {
				buckets = append(buckets, current)
			}
			return
		}
		for b := start; b < mihSegWidth; b++ {
			flipBits(b+1, remaining-1, current^(1<<uint(b)))
		}
	}
	for k := 1; k <= t; k++ {
		flipBits(0, k, val)
	}
	return buckets
}

// comb 计算组合数 C(n, k)。
func comb(n, k int) int {
	if k > n || k < 0 {
		return 0
	}
	if k == 0 || k == n {
		return 1
	}
	if k > n-k {
		k = n - k
	}
	result := 1
	for i := 0; i < k; i++ {
		result = result * (n - i) / (i + 1)
	}
	return result
}
