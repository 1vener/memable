// sprite.go：Stash 风格视频 sprite pHash（25 张临时截图 → 5×5 sprite → pHash → 清理）。
// 代码注释使用中文。
package media

import (
	"context"
	"fmt"
	"image"
	"image/draw"
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
const spriteSegments = 5     // 分段快速抽帧段数；每段抽 5 帧，共 25 帧

// ComputeVideoSpritePHash 生成视频 sprite pHash。
// 临时截图和 sprite 图仅在计算过程中存在，完成后立即删除。
func ComputeVideoSpritePHash(ctx context.Context, videoPath string, durationMs int64) (string, error) {
	return ComputeVideoSpritePHashAndCover(ctx, videoPath, durationMs, "", 0)
}

// ComputeVideoSpritePHashAndCover 一次生成 sprite pHash 和视频封面。
// 封面从 sprite 中间帧开始向两侧选择最近的非黑屏/非纯色帧，避免再次读取视频。
func ComputeVideoSpritePHashAndCover(ctx context.Context, videoPath string, durationMs int64, coverPath string, maxEdge int) (string, error) {
	if durationMs <= 0 {
		return "", fmt.Errorf("视频时长无效: %d", durationMs)
	}

	tmpDir, err := os.MkdirTemp("", "memable-sprite-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmpDir) // 确保临时文件全部清理

	spritePath := filepath.Join(tmpDir, "sprite.png")
	built := false
	// 主路径：分段快速抽帧（5 段 × 每段 1 秒窗口抽 5 帧），避免解码整段视频。
	if strips, serr := extractSpriteSegments(ctx, videoPath, durationMs, tmpDir); serr == nil && len(strips) == spriteSegments {
		if err := buildSpriteFromStrips(strips, spritePath); err != nil {
			return "", fmt.Errorf("拼接分段 sprite: %w", err)
		}
		built = true
	}
	if !built {
		// 回退：单条 trim+fps+tile 整段抽帧 → 逐帧抽取 + Go 拼接
		if err := extractSpriteTile(ctx, videoPath, durationMs, spritePath); err != nil {
			frames, ferr := extractSpriteFrames(ctx, videoPath, durationMs, tmpDir)
			if ferr != nil || len(frames) != spriteCount {
				return "", fmt.Errorf("抽取 sprite 截图: %w", err)
			}
			if err := buildSprite(frames, spritePath); err != nil {
				return "", fmt.Errorf("拼接 sprite: %w", err)
			}
		}
	}

	f, err := os.Open(spritePath)
	if err != nil {
		return "", fmt.Errorf("打开 sprite: %w", err)
	}
	sprite, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("解码 sprite: %w", err)
	}
	phash := pHash(sprite)

	if coverPath != "" {
		cover, err := selectSpriteCover(sprite)
		if err != nil {
			return "", err
		}
		if err := GenerateThumbnailFromImage(cover, coverPath, maxEdge); err != nil {
			return "", fmt.Errorf("生成 sprite 封面: %w", err)
		}
	}
	return phash, nil
}

// extractSpriteSegments 分段快速抽帧（主路径）：
// 把 5%~95% 区间等分为 5 段，每段用 -ss（放在 -i 前，借助关键帧索引快速定位）
// 只解码段中点附近约 1 秒窗口，fps=5 抽 5 帧并 tile=5x1 拼成横条。
// 长视频解码量从 ~90% 时长降到 ~5 秒，避免整段解码导致的超时与回退。
func extractSpriteSegments(ctx context.Context, videoPath string, durationMs int64, outDir string) ([]string, error) {
	d := float64(durationMs) / 1000.0
	segSpan := d * 0.90 / float64(spriteSegments)
	if segSpan <= 0 {
		return nil, fmt.Errorf("分段区间无效: duration=%.3f", d)
	}
	strips := make([]string, 0, spriteSegments)
	for i := 0; i < spriteSegments; i++ {
		select {
		case <-ctx.Done():
			return strips, ctx.Err()
		default:
		}
		mid := d*0.05 + float64(i)*segSpan + segSpan/2
		start := mid - 0.5
		if start < 0 {
			start = 0
		}
		outPath := filepath.Join(outDir, fmt.Sprintf("seg_%02d.png", i))
		if err := ffmpegSegmentStrip(ctx, videoPath, start, outPath); err != nil {
			return strips, err
		}
		strips = append(strips, outPath)
	}
	return strips, nil
}

// ffmpegSegmentStrip 用 -ss 快速定位后解码 1 秒窗口，fps=5 抽 5 帧并拼成 1×5 横条 PNG。
func ffmpegSegmentStrip(ctx context.Context, videoPath string, startSec float64, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filter := fmt.Sprintf("fps=5,scale=%d:-1,tile=5x1", spriteFrameWidth)
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-v", "error",
		"-ss", fmt.Sprintf("%.3f", startSec),
		"-i", videoPath,
		"-t", "1",
		"-vf", filter,
		"-frames:v", "1",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 分段抽帧 t=%.3f: %w\n%s", startSec, err, string(out))
	}
	return nil
}

// buildSpriteFromStrips 将 5 段 1×5 横条图按行拼成 5×5 sprite 图（分段抽帧主路径）。
func buildSpriteFromStrips(stripPaths []string, outPath string) error {
	if len(stripPaths) != spriteSegments {
		return fmt.Errorf("分段条数不等于 %d", spriteSegments)
	}
	var sprite *image.RGBA
	var fw, fh int
	for i, fp := range stripPaths {
		f, err := os.Open(fp)
		if err != nil {
			return fmt.Errorf("打开分段条 %d: %w", i, err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("解码分段条 %d: %w", i, err)
		}
		b := img.Bounds()
		if sprite == nil {
			fw = b.Dx() / spriteCols
			fh = b.Dy()
			if fw <= 0 || fh <= 0 {
				return fmt.Errorf("分段条尺寸无效: %dx%d", b.Dx(), b.Dy())
			}
			sprite = image.NewRGBA(image.Rect(0, 0, fw*spriteCols, fh*spriteRows))
		}
		if b.Dx() != fw*spriteCols || b.Dy() != fh {
			return fmt.Errorf("分段条 %d 尺寸异常: %dx%d", i, b.Dx(), b.Dy())
		}
		for j := 0; j < spriteCols; j++ {
			draw.Draw(sprite, image.Rect(j*fw, i*fh, (j+1)*fw, (i+1)*fh), img,
				image.Pt(j*fw, 0), draw.Src)
		}
	}
	if sprite == nil {
		return fmt.Errorf("没有可用的分段条")
	}
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建 sprite 文件: %w", err)
	}
	defer out.Close()
	return png.Encode(out, sprite)
}

// selectSpriteCover 优先取第 13 帧（50% 附近），无效时向前后寻找最近候选。
func selectSpriteCover(sprite image.Image) (image.Image, error) {
	b := sprite.Bounds()
	fw, fh := b.Dx()/spriteCols, b.Dy()/spriteRows
	if fw <= 0 || fh <= 0 {
		return nil, fmt.Errorf("sprite 尺寸无效: %dx%d", b.Dx(), b.Dy())
	}
	center := spriteCount / 2
	for distance := 0; distance < spriteCount; distance++ {
		indices := []int{center - distance}
		if distance > 0 {
			indices = append(indices, center+distance)
		}
		for _, index := range indices {
			if index < 0 || index >= spriteCount {
				continue
			}
			x := b.Min.X + index%spriteCols*fw
			y := b.Min.Y + index/spriteCols*fh
			frame := cropImage(sprite, image.Rect(x, y, x+fw, y+fh))
			if !isBlackOrSolidImage(frame) {
				return frame, nil
			}
		}
	}
	return nil, fmt.Errorf("sprite 中没有有效封面帧")
}

func cropImage(src image.Image, rect image.Rectangle) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
	return dst
}

// extractSpriteTile 用单条 ffmpeg 命令完成 25 帧抽取 + 缩放 + 5×5 拼接。
// 关键滤镜链：select 关键帧 → scale 缩放 → tile 拼接 → 输出单张 PNG。
// 相比逐帧 25 次 ffmpeg 调用，子进程数从 25 降到 1，总耗时大幅下降。
func extractSpriteTile(ctx context.Context, videoPath string, durationMs int64, outPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	d := float64(durationMs) / 1000.0
	start := d * 0.05
	end := d * 0.95
	// 25 帧均匀采样：fps = frames / duration_span
	span := end - start
	if span <= 0 {
		return fmt.Errorf("采样区间无效: start=%.3f end=%.3f", start, end)
	}
	fps := float64(spriteCount) / span

	// 滤镜链：
	//   1. trim 截取 5%~95% 区间（避免开头黑屏/结尾字幕干扰）
	//   2. fps 均匀采样 25 帧
	//   3. scale 缩放到固定宽度（保持宽高比）
	//   4. tile=5x5 拼接成 sprite
	filter := fmt.Sprintf(
		"trim=start=%.3f:end=%.3f,fps=%.6f,scale=%d:-1,tile=%dx%d",
		start, end, fps, spriteFrameWidth, spriteCols, spriteRows,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-v", "error",
		"-i", videoPath,
		"-frames:v", "1",
		"-vf", filter,
		"-update", "1",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg tile 抽帧: %w\n%s", err, string(out))
	}
	return nil
}

// extractSpriteFrames 在 duration 的 5%~95% 区间逐帧抽取 25 张截图（回退路径）。
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

// buildSprite 将 25 张截图拼接成 5×5 sprite 图（回退路径）。
// 使用 draw.Draw 批量拷贝，避免逐像素 Set/At 的方法调用开销。
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
		row := i / spriteCols // 修复：原为 i / spriteRows，动态网格下应为 i / cols
		ox := col * fw
		oy := row * fh

		// 批量拷贝整帧，比逐像素 Set 快一个数量级
		draw.Draw(sprite, image.Rect(ox, oy, ox+fw, oy+fh), img, image.Point{}, draw.Src)
	}

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建 sprite 文件: %w", err)
	}
	defer out.Close()
	return png.Encode(out, sprite)
}
