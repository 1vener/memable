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
	"path/filepath"
	"time"

	"memable/internal/cmdx"
)

// CoverResult 封面提取结果。
type CoverResult struct {
	ThumbnailPath string // 落盘后的缩略图路径
	UsedTimeMs    int64  // 实际使用的封面时间点
}

// ExtractVideoCover 从视频中提取封面并生成缩略图到 outputPath。
// 规则：
//   - duration < 10s → 50%；10s~60s → 30%；>= 60s → 10%
//   - 黑屏/近纯色时在该时间点前后按 10% duration 间隔重试，最多 5 个时间点
//   - 仍失败时回退到 50% duration，最后回退到 0s
//   - 临时抽帧文件使用 os.CreateTemp 避免多 worker 冲突
func ExtractVideoCover(ctx context.Context, videoPath, outputPath string, maxEdge int, durationMs int64) (*CoverResult, error) {
	if maxEdge <= 0 {
		maxEdge = 400
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建封面目录: %w", err)
	}

	candidates := buildCoverCandidates(durationMs)

	for _, tMs := range candidates {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		rawFile, err := os.CreateTemp("", "cover-raw-*.jpg")
		if err != nil {
			return nil, fmt.Errorf("创建临时抽帧文件: %w", err)
		}
		rawPath := rawFile.Name()
		rawFile.Close()

		if err := ffmpegExtractFrame(ctx, videoPath, tMs, rawPath); err != nil {
			_ = os.Remove(rawPath)
			continue
		}

		if isBlackOrSolid(rawPath) {
			_ = os.Remove(rawPath)
			continue
		}

		err = GenerateImageThumbnail(rawPath, outputPath, maxEdge)
		_ = os.Remove(rawPath)
		if err != nil {
			continue
		}
		return &CoverResult{ThumbnailPath: outputPath, UsedTimeMs: tMs}, nil
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
// 先尝试快速定位（-ss 在 -i 前），失败后重试准确但较慢的定位（-ss 在 -i 后）。
func ffmpegExtractFrame(ctx context.Context, videoPath string, timeMs int64, outPath string) error {
	sec := float64(timeMs) / 1000.0
	ts := fmt.Sprintf("%.3f", sec)

	// 第一次尝试：快速定位
	fastCtx, fastCancel := context.WithTimeout(ctx, 15*time.Second)
	defer fastCancel()
	cmd := cmdx.Command(fastCtx, "ffmpeg",
		"-y",
		"-ss", ts,
		"-i", videoPath,
		"-frames:v", "1",
		"-q:v", "2",
		outPath,
	)
	if _, err := cmd.CombinedOutput(); err == nil {
		return nil
	}

	// 第二次尝试：准确定位（-ss 在 -i 后）
	preciseCtx, preciseCancel := context.WithTimeout(ctx, 30*time.Second)
	defer preciseCancel()
	cmd2 := cmdx.Command(preciseCtx, "ffmpeg",
		"-y",
		"-i", videoPath,
		"-ss", ts,
		"-frames:v", "1",
		"-q:v", "2",
		outPath,
	)
	if out, err := cmd2.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 抽帧 t=%.3f: %w\n%s", sec, err, string(out))
	}
	return nil
}

// isBlackOrSolid 检测图片是否为黑屏或近纯色。
// 判据：像素均值 < 8（黑屏）或相对标准差 < 5%（近纯色）。
// 相对判据（标准差 / 均值）避免"偏暗但有效"的视频帧被误判为近纯色：
// 暗场景均值低、绝对标准差天然小，但若内容有对比（stddev 与 mean 同量级）仍应视为有效。
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
	return isBlackOrSolidImage(img)
}

func isBlackOrSolidImage(img image.Image) bool {
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
	// 近纯色：相对标准差 < 5%（标准差显著小于均值）。
	// 纯黑帧由 mean < 8 拦截；这里对"暗但有对比"的帧放行。
	return stddev < mean*0.05
}

// ReplaceExt 替换文件扩展名。
func ReplaceExt(name, newExt string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name + newExt
	}
	return name[:len(name)-len(ext)] + newExt
}
