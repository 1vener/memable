// mih_test.go：MIH 索引正确性测试。
package duplicate

import (
	"fmt"
	"math/bits"
	"math/rand"
	"testing"

	"memable/internal/media"
	"memable/internal/repo"
)

func hashHex(h uint64) string {
	return fmt.Sprintf("%016x", h)
}

func TestMIHQueryFindsAllWithinDistance(t *testing.T) {
	// 构造 1000 个随机 pHash，验证 MIH 查询不遗漏距离 ≤ d 的对。
	rng := rand.New(rand.NewSource(42))
	n := 1000
	hashes := make([]uint64, n)
	items := make([]repo.Media, n)
	for i := 0; i < n; i++ {
		h := rng.Uint64()
		hashes[i] = h
		s := hashHex(h)
		items[i] = repo.Media{ID: int64(i + 1), Phash: &s}
	}

	d := 6
	idx := NewPhashIndex(items)

	// 暴力 O(n²) 结果作为基准
	brutePairs := map[[2]int]bool{}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if media.HammingUint64(hashes[i], hashes[j]) <= d {
				brutePairs[[2]int{i, j}] = true
			}
		}
	}

	// MIH 结果
	mihPairs := map[[2]int]bool{}
	candidates := idx.QueryAll(d, nil)
	for i, cands := range candidates {
		for _, j := range cands {
			if media.HammingUint64(hashes[i], hashes[j]) <= d {
				mihPairs[[2]int{i, j}] = true
			}
		}
	}

	// 检查无遗漏
	for pair := range brutePairs {
		if !mihPairs[pair] {
			t.Fatalf("MIH 遗漏了暴力结果中的对 (%d, %d)", pair[0], pair[1])
		}
	}
	t.Logf("暴力 %d 对，MIH 候选验证 %d 对，无遗漏", len(brutePairs), len(mihPairs))
}

func TestMIHQueryReturnsCorrectCandidates(t *testing.T) {
	// 构造已知距离的 pHash 对，验证 MIH 返回正确候选。
	h1 := uint64(0x0000000000000000)
	h2 := h1 ^ 0x0000000000000003 // 距离 2（翻转低 2 bit）
	h3 := h1 ^ 0xFFFFFFFFFFFFFFFF // 距离 64

	s1 := hashHex(h1)
	s2 := hashHex(h2)
	s3 := hashHex(h3)

	items := []repo.Media{
		{ID: 1, Phash: &s1},
		{ID: 2, Phash: &s2},
		{ID: 3, Phash: &s3},
	}

	idx := NewPhashIndex(items)

	// 查询 h1，距离 ≤ 6：应返回索引 1（h2），不返回索引 2（h3）
	cands := idx.Query(h1, 6)
	found := false
	for _, c := range cands {
		if c == 1 {
			found = true
		}
		if c == 2 {
			t.Fatal("不应返回距离 64 的候选")
		}
	}
	if !found {
		t.Fatal("应返回距离 2 的候选")
	}
}

func TestMIHSegmentsAndBuckets(t *testing.T) {
	// 验证 segValue 提取正确。
	h := uint64(0x1234567890ABCDEF)
	// 4 段 × 16 bit：seg0 = 0xCDEF, seg1 = 0x90AB, seg2 = 0x5678, seg3 = 0x1234
	expected := []uint32{0xCDEF, 0x90AB, 0x5678, 0x1234}
	for seg, want := range expected {
		got := segValue(h, seg)
		if got != want {
			t.Errorf("segValue(h, %d) = %04X, 应为 %04X", seg, got, want)
		}
	}
}

func TestMIHEnumerateBuckets(t *testing.T) {
	// 验证枚举数量：距离 1 时应有 1 + 16 = 17 个值。
	buckets := enumerateBuckets(0, 1)
	if len(buckets) != 17 {
		t.Fatalf("enumerateBuckets(0, 1) 应返回 17 个值，实际 %d", len(buckets))
	}

	// 验证包含自身
	found := false
	for _, b := range buckets {
		if b == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("enumerateBuckets 应包含自身")
	}

	// 验证距离 0 只返回自身
	buckets0 := enumerateBuckets(42, 0)
	if len(buckets0) != 1 || buckets0[0] != 42 {
		t.Fatalf("enumerateBuckets(42, 0) 应只返回 [42]，实际 %v", buckets0)
	}
}

func TestMIHHammingUint64(t *testing.T) {
	cases := []struct {
		a, b uint64
		want int
	}{
		{0, 0, 0},
		{0, 1, 1},
		{0, 3, 2},
		{0, 0xFFFFFFFFFFFFFFFF, 64},
		{0x1234567890ABCDEF, 0x1234567890ABCDEF, 0},
	}
	for _, c := range cases {
		got := media.HammingUint64(c.a, c.b)
		if got != c.want {
			t.Errorf("HammingUint64(%016X, %016X) = %d, 应为 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMIHParseHex64(t *testing.T) {
	cases := []struct {
		s    string
		want uint64
	}{
		{"0000000000000000", 0},
		{"0000000000000001", 1},
		{"ffffffffffffffff", 0xFFFFFFFFFFFFFFFF},
		{"1234567890abcdef", 0x1234567890ABCDEF},
	}
	for _, c := range cases {
		got, err := media.ParseHex64(c.s)
		if err != nil {
			t.Errorf("ParseHex64(%q) 错误: %v", c.s, err)
		}
		if got != c.want {
			t.Errorf("ParseHex64(%q) = %016X, 应为 %016X", c.s, got, c.want)
		}
	}
}

func TestMIHLargeScaleNoFalseNegatives(t *testing.T) {
	// 10000 个项目，距离 12，验证无遗漏。
	if testing.Short() {
		t.Skip("跳过大规模测试")
	}
	rng := rand.New(rand.NewSource(123))
	n := 10000
	hashes := make([]uint64, n)
	items := make([]repo.Media, n)
	for i := 0; i < n; i++ {
		h := rng.Uint64()
		hashes[i] = h
		s := hashHex(h)
		items[i] = repo.Media{ID: int64(i + 1), Phash: &s}
	}

	d := 12
	idx := NewPhashIndex(items)

	// 暴力基准（只统计数量，不存全部对以省内存）
	bruteCount := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if bits.OnesCount64(hashes[i]^hashes[j]) <= d {
				bruteCount++
			}
		}
	}

	// MIH 验证
	mihCount := 0
	candidates := idx.QueryAll(d, nil)
	for i, cands := range candidates {
		for _, j := range cands {
			if bits.OnesCount64(hashes[i]^hashes[j]) <= d {
				mihCount++
			}
		}
	}

	if bruteCount != mihCount {
		t.Fatalf("暴力 %d 对 vs MIH %d 对，存在遗漏", bruteCount, mihCount)
	}
	t.Logf("10000 项目，距离 %d，%d 对，MIH 无遗漏", d, bruteCount)
}
