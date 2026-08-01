// perceptual.go：图片感知哈希（aHash/dHash/pHash）。
// 代码注释使用中文。
package media

import (
	"context"
	"fmt"
	"image"
	"math"
	"math/bits"
	"sort"
	"strconv"
)

// ImageHashes 图片相似度哈希，均为 64 bit 的 16 进制字符串。
type ImageHashes struct {
	AHash string
	DHash string
	PHash string
}

// ImagePerceptualHashes 从文件路径计算图片 aHash/dHash/pHash。
func ImagePerceptualHashes(path string) (*ImageHashes, error) {
	decoded, err := DecodeImage(context.Background(), path, DecoderGo)
	if err != nil {
		return nil, err
	}
	return ImagePerceptualHashesFromImage(decoded.Image), nil
}

// HammingHex64 计算两个 64 bit 十六进制哈希的 Hamming 距离。
func HammingHex64(a, b string) (int, error) {
	var x, y uint64
	if _, err := fmt.Sscanf(a, "%016x", &x); err != nil {
		return 0, err
	}
	if _, err := fmt.Sscanf(b, "%016x", &y); err != nil {
		return 0, err
	}
	return bits.OnesCount64(x ^ y), nil
}

// ParseHex64 将 64 bit 十六进制哈希字符串解析为 uint64。
// 比 fmt.Sscanf 快约 10-20 倍；检测前预解析可大幅减少比较开销。
func ParseHex64(s string) (uint64, error) {
	return strconv.ParseUint(s, 16, 64)
}

// HammingUint64 计算两个 uint64 的 Hamming 距离（纯位运算，无字符串解析）。
func HammingUint64(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

func aHash(img image.Image) string {
	gray := resizeGray(img, 8, 8)
	var sum int
	for _, v := range gray {
		sum += int(v)
	}
	avg := sum / len(gray)
	var h uint64
	for _, v := range gray {
		h <<= 1
		if int(v) >= avg {
			h |= 1
		}
	}
	return fmt.Sprintf("%016x", h)
}

func dHash(img image.Image) string {
	gray := resizeGray(img, 9, 8)
	var h uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			h <<= 1
			if gray[y*9+x] > gray[y*9+x+1] {
				h |= 1
			}
		}
	}
	return fmt.Sprintf("%016x", h)
}

func pHash(img image.Image) string {
	gray := resizeGray(img, 32, 32)
	coeffs := dct2D(gray, 32)
	vals := make([]float64, 0, 63)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x == 0 && y == 0 {
				continue // 跳过 DC 分量，避免亮度主导
			}
			vals = append(vals, coeffs[y*32+x])
		}
	}
	median := medianFloat(vals)

	var h uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			h <<= 1
			if x == 0 && y == 0 {
				continue
			}
			if coeffs[y*32+x] >= median {
				h |= 1
			}
		}
	}
	return fmt.Sprintf("%016x", h)
}

// resizeGray 使用最近邻缩放并转灰度；不引入第三方依赖。
func resizeGray(img image.Image, w, h int) []uint8 {
	b := img.Bounds()
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			r, g, bl, _ := img.At(sx, sy).RGBA()
			// Rec.601 灰度转换；RGBA 返回 16 bit 值。
			gray := (299*r + 587*g + 114*bl) / 1000 / 257
			out[y*w+x] = uint8(gray)
		}
	}
	return out
}

func dct2D(vals []uint8, n int) []float64 {
	// 二维 DCT 可分离为先按行、再按列两次一维 DCT，将复杂度从 O(n^4) 降为 O(n^3)。
	cosTable := make([]float64, n*n)
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			cosTable[k*n+i] = math.Cos((float64(2*i+1) * float64(k) * math.Pi) / (2 * float64(n)))
		}
	}
	tmp := make([]float64, n*n)
	for y := 0; y < n; y++ {
		for u := 0; u < n; u++ {
			var sum float64
			for x := 0; x < n; x++ {
				sum += float64(vals[y*n+x]) * cosTable[u*n+x]
			}
			tmp[y*n+u] = alpha(u, n) * sum
		}
	}
	out := make([]float64, n*n)
	for u := 0; u < n; u++ {
		for v := 0; v < n; v++ {
			var sum float64
			for y := 0; y < n; y++ {
				sum += tmp[y*n+u] * cosTable[v*n+y]
			}
			out[v*n+u] = alpha(v, n) * sum
		}
	}
	return out
}

func alpha(k, n int) float64 {
	if k == 0 {
		return math.Sqrt(1 / float64(n))
	}
	return math.Sqrt(2 / float64(n))
}

func medianFloat(vals []float64) float64 {
	cpy := append([]float64(nil), vals...)
	sort.Float64s(cpy)
	mid := len(cpy) / 2
	if len(cpy)%2 == 0 {
		return (cpy[mid-1] + cpy[mid]) / 2
	}
	return cpy[mid]
}
