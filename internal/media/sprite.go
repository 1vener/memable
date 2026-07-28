// sprite.go：Stash 风格视频 sprite pHash（25 张临时截图 → 5×5 sprite → pHash → 清理）。
// 代码注释使用中文。
package media

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const spriteCount = 25       // 截图数量
const spriteCols = 5         // sprite 列数
const spriteRows = 5         // sprite 行数
const spriteFrameWidth = 160 // 每张截图缩放后的宽度（像素）

// ComputeVideoSpritePHash 生成视频 sprite pHash。
// 临时截图和 sprite 图仅在计算过程中存在，完成后立即删除。
func ComputeVideoSpritePHash(ctx context.Context, videoPath string, durationMs int64) (string, error) {
	if durationMs <= 0 {
		return "", fmt.Errorf("视频时长无效: %d", durationMs)
	}

	tmpDir, err := os.MkdirTemp("", "memable-sprite-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmpDir) // 确保临时文件全部清理

	// 1. 在 5%~95% 区间均匀抽取 25 张截图
	frames, err := extractSpriteFrames(ctx, videoPath, durationMs, tmpDir)
	if err != nil {
		return "", fmt.Errorf("抽取 sprite 截图: %w", err)
	}
	if len(frames) != spriteCount {
		return "", fmt.Errorf("截图数量不足: 期望 %d, 实际 %d", spriteCount, len(frames))
	}

	// 2. 拼接成 5×5 sprite 图
	spritePath := filepath.Join(tmpDir, "sprite.png")
	if err := buildSprite(frames, spritePath); err != nil {
		return "", fmt.Errorf("拼接 sprite: %w", err)
	}

	// 3. 对 sprite 图计算 pHash（复用图片 pHash 逻辑）
	phash, err := computePHashFromFile(spritePath)
	if err != nil {
		return "", fmt.Errorf("计算 sprite pHash: %w", err)
	}

	return phash, nil
}

// extractSpriteFrames 在 duration 的 5%~95% 区间均匀抽取 25 张截图。
func extractSpriteFrames(ctx context.Context, videoPath string, durationMs int64, outDir string) ([]string, error) {
	d := float64(durationMs) / 1000.0
	offset := d * 0.05
	step := d * 0.90 / float64(spriteCount)

	frames := make([]string, 0, spriteCount)
	for i := 0; i < spriteCount; i++ {
		t := offset + float64(i)*step
		outPath := filepath.Join(outDir, fmt.Sprintf("frame_%02d.png", i))

		select {
		case <-ctx.Done():
			return frames, ctx.Err()
		default:
		}

		if err := ffmpegScaleFrame(ctx, videoPath, t, spriteFrameWidth, outPath); err != nil {
			// 单帧失败时跳过，不影响整体（但最终数量不足会报错）
			continue
		}
		frames = append(frames, outPath)
	}
	return frames, nil
}

// ffmpegScaleFrame 用 ffmpeg 在指定时间点抽取一帧并缩放到指定宽度。
func ffmpegScaleFrame(ctx context.Context, videoPath string, sec float64, width int, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filter := fmt.Sprintf("scale=%d:-1", width)
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.3f", sec),
		"-i", videoPath,
		"-frames:v", "1",
		"-vf", filter,
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 抽帧 t=%.3f: %w\n%s", sec, err, string(out))
	}
	return nil
}

// buildSprite 将 25 张截图拼接成 5×5 sprite 图。
func buildSprite(framePaths []string, outPath string) error {
	if len(framePaths) != spriteCount {
		return fmt.Errorf("截图数量不等于 %d", spriteCount)
	}

	// 读取第一张获取单帧高度
	firstFile, err := os.Open(framePaths[0])
	if err != nil {
		return fmt.Errorf("打开首帧: %w", err)
	}
	img0, _, err := image.Decode(firstFile)
	firstFile.Close()
	if err != nil {
		return fmt.Errorf("解码首帧: %w", err)
	}
	fw := img0.Bounds().Dx()
	fh := img0.Bounds().Dy()

	spriteW := fw * spriteCols
	spriteH := fh * spriteRows
	sprite := image.NewRGBA(image.Rect(0, 0, spriteW, spriteH))

	for i, fp := range framePaths {
		f, err := os.Open(fp)
		if err != nil {
			return fmt.Errorf("打开帧 %d: %w", i, err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("解码帧 %d: %w", i, err)
		}

		col := i % spriteCols
		row := i / spriteRows
		ox := col * fw
		oy := row * fh

		for y := 0; y < fh; y++ {
			for x := 0; x < fw; x++ {
				sprite.Set(ox+x, oy+y, img.At(x, y))
			}
		}
	}

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建 sprite 文件: %w", err)
	}
	defer out.Close()
	return png.Encode(out, sprite)
}

// computePHashFromFile 从图片文件计算 pHash。
func computePHashFromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}
	return pHash(img), nil
}
