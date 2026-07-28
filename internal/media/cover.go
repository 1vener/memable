// cover.go：视频封面图提取（ffmpeg 抽帧 + 黑屏/近纯色检测 + 回退重试）。
// 代码注释使用中文。
package media

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CoverResult 封面提取结果。
type CoverResult struct {
	ThumbnailPath string // 落盘后的缩略图路径
	UsedTimeMs    int64  // 实际使用的封面时间点
}

// ExtractVideoCover 从视频中提取封面并生成缩略图。
// 规则：
//   - duration < 10s → 50%；10s~60s → 30%；>= 60s → 10%
//   - 黑屏/近纯色时在该时间点前后按 10% duration 间隔重试，最多 5 个时间点
//   - 仍失败时回退到 50% duration，最后回退到 0s
func ExtractVideoCover(ctx context.Context, videoPath, outDir string, maxEdge int, durationMs int64) (*CoverResult, error) {
	if maxEdge <= 0 {
		maxEdge = 300
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建封面目录: %w", err)
	}

	// 候选时间点列表
	candidates := buildCoverCandidates(durationMs)

	for _, tMs := range candidates {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rawPath := filepath.Join(outDir, "_cover_raw.jpg")
		if err := ffmpegExtractFrame(ctx, videoPath, tMs, rawPath); err != nil {
			// 抽帧失败，尝试下一个时间点
			_ = os.Remove(rawPath)
			continue
		}

		if isBlackOrSolid(rawPath) {
			_ = os.Remove(rawPath)
			continue
		}

		// 有效帧：resize 后保存为缩略图
		thumbName := fmt.Sprintf("cover_%s", filepath.Base(videoPath))
		thumbName = replaceExt(thumbName, ".png")
		thumbPath := filepath.Join(outDir, thumbName)
		if err := GenerateImageThumbnail(rawPath, thumbPath, maxEdge); err != nil {
			_ = os.Remove(rawPath)
			return nil, fmt.Errorf("生成封面缩略图: %w", err)
		}
		_ = os.Remove(rawPath)
		return &CoverResult{ThumbnailPath: thumbPath, UsedTimeMs: tMs}, nil
	}

	return nil, fmt.Errorf("视频 %q 在 %d 个时间点均无法提取有效封面", videoPath, len(candidates))
}

// buildCoverCandidates 按规则生成封面候选时间点序列。
func buildCoverCandidates(durationMs int64) []int64 {
	if durationMs <= 0 {
		return []int64{0}
	}
	d := float64(durationMs)
	var baseRatio float64
	switch {
	case durationMs < 10_000:
		baseRatio = 0.50
	case durationMs < 60_000:
		baseRatio = 0.30
	default:
		baseRatio = 0.10
	}
	baseMs := int64(d * baseRatio)
	stepMs := int64(d * 0.10) // 10% duration 间隔

	// 候选序列：base, base±step, base±2step ...（最多 5 个）
	candidates := []int64{baseMs}
	for i := 1; len(candidates) < 5; i++ {
		after := baseMs + stepMs*int64(i)
		before := baseMs - stepMs*int64(i)
		if before >= 0 {
			candidates = append(candidates, before)
		}
		if after <= durationMs {
			candidates = append(candidates, after)
		}
		if before < 0 && after > durationMs {
			break
		}
	}

	// 追加回退：50% duration，最后 0s
	halfMs := int64(d * 0.50)
	if halfMs != baseMs && halfMs > 0 && halfMs < durationMs {
		candidates = append(candidates, halfMs)
	}
	candidates = append(candidates, 0)

	return candidates
}

// ffmpegExtractFrame 用 ffmpeg 在指定时间点抽取单张 jpg。
func ffmpegExtractFrame(ctx context.Context, videoPath string, timeMs int64, outPath string) error {
	sec := float64(timeMs) / 1000.0
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.3f", sec),
		"-i", videoPath,
		"-frames:v", "1",
		"-q:v", "2",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 抽帧 t=%.3f: %w\n%s", sec, err, string(out))
	}
	return nil
}

// isBlackOrSolid 检测图片是否为黑屏或近纯色。
// 判据：像素均值 < 8（黑屏）或所有像素与均值的标准差 < 2（近纯色）。
func isBlackOrSolid(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true // 无法打开则视为无效
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return true
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return true
	}

	// 采样：每 4 个像素取一个，降低计算量
	var sum, count float64
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			lum := float64(r+g+bl) / 3.0 / 257.0
			sum += lum
			count++
		}
	}
	if count == 0 {
		return true
	}
	mean := sum / count
	if mean < 8 {
		return true // 近黑屏
	}

	// 计算标准差
	var sqSum float64
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			lum := float64(r+g+bl) / 3.0 / 257.0
			diff := lum - mean
			sqSum += diff * diff
		}
	}
	stddev := math.Sqrt(sqSum / count)
	return stddev < 2.0 // 近纯色
}

func replaceExt(name, newExt string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name + newExt
	}
	return name[:len(name)-len(ext)] + newExt
}
