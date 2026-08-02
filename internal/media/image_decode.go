// image_decode.go：统一图片解码入口（Go 原生 + FFmpeg 转码）。
// 代码注释使用中文。
package media

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	_ "golang.org/x/image/bmp"
)

// maxDirectDecodePixels 直接使用 Go 原生解码的最大像素数。
// 超过该值（高像素相机照片、拼接全景图等）先经 FFmpeg 限分辨率转码再解码，
// 避免 processSOS 等解码阶段一次性分配数百 MB 内存导致进程 OOM。
const maxDirectDecodePixels = 24_000_000

// maxScaledDecodeEdge FFmpeg 限分辨率解码输出的最大边长（像素）。
// 感知哈希仅需 32×32、缩略图默认 400px，3000px 边长已足够且内存可控。
const maxScaledDecodeEdge = 3000

// DecodedImage 解码后的图片。
type DecodedImage struct {
	Image  image.Image
	Format string
}

// DecodeImage 统一图片解码入口，根据 decoder 策略调用 Go 原生或 FFmpeg 临时转换。
func DecodeImage(ctx context.Context, path string, decoder DecoderKind) (*DecodedImage, error) {
	switch decoder {
	case DecoderGo:
		decoded, err := decodeGoImage(ctx, path)
		if err == nil {
			return decoded, nil
		}
		// 扩展名声明为 Go 原生格式，但内容无法识别（如实际为 WebP/JPEG2000/HEIC
		// 却使用 .jpg 扩展名，或文件头损坏）。回退 FFmpeg 转码再解码；
		// FFmpeg 也失败时保留原始错误（该文件记为失败，不中断扫描）。
		img, format, ferr := decodeImageWithFFmpeg(ctx, path)
		if ferr != nil {
			return nil, err
		}
		return &DecodedImage{Image: img, Format: format}, nil
	case DecoderFFmpeg:
		img, format, err := decodeImageWithFFmpeg(ctx, path)
		if err != nil {
			return nil, err
		}
		return &DecodedImage{Image: img, Format: format}, nil
	default:
		return nil, fmt.Errorf("不支持的解码器类型: %q", decoder)
	}
}

// decodeGoImage 使用 Go 标准/扩展库解码图片（jpg/jpeg/jfif/png/bmp）。
// 先读取头部尺寸：超过 maxDirectDecodePixels 的图片改走 FFmpeg 限分辨率解码，
// 防止超大图片全分辨率解码耗尽内存（32 位进程堆上限约 2GB）。
func decodeGoImage(ctx context.Context, path string) (*DecodedImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开图片 %q: %w", path, err)
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("解码图片 %q: %w", path, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("重置图片读取位置 %q: %w", path, err)
	}

	if cfg.Width > 0 && cfg.Height > 0 &&
		uint64(cfg.Width)*uint64(cfg.Height) > maxDirectDecodePixels {
		img, format, err := decodeImageWithFFmpegScaled(ctx, path, cfg.Width, cfg.Height)
		if err != nil {
			return nil, fmt.Errorf("缩放解码超大图片 %q (%dx%d): %w", path, cfg.Width, cfg.Height, err)
		}
		return &DecodedImage{Image: img, Format: format}, nil
	}

	img, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("解码图片 %q: %w", path, err)
	}
	return &DecodedImage{Image: img, Format: format}, nil
}

// decodeImageWithFFmpeg 通过 FFmpeg 将源文件转为临时 PNG 再用 Go 解码。
// 适用于 HEIC、CR2 等解码器不直接支持的格式。
// 单文件超时 60 秒；若 ctx 先到期则以 ctx 为准。
func decodeImageWithFFmpeg(ctx context.Context, srcPath string) (image.Image, string, error) {
	return decodeImageWithFFmpegFilter(ctx, srcPath, "")
}

// decodeImageWithFFmpegScaled 通过 FFmpeg 限分辨率转码超大图片（scale 滤镜），
// 输出最长边不超过 maxScaledDecodeEdge 的临时 PNG 后再解码。
func decodeImageWithFFmpegScaled(ctx context.Context, srcPath string, srcW, srcH int) (image.Image, string, error) {
	var tw, th int
	if srcW >= srcH {
		tw = maxScaledDecodeEdge
		th = srcH * maxScaledDecodeEdge / srcW
	} else {
		th = maxScaledDecodeEdge
		tw = srcW * maxScaledDecodeEdge / srcH
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	return decodeImageWithFFmpegFilter(ctx, srcPath, fmt.Sprintf("scale=%d:%d", tw, th))
}

// decodeImageWithFFmpegFilter 执行 FFmpeg 转码并解码临时 PNG；vf 非空时附加 -vf 滤镜。
func decodeImageWithFFmpegFilter(ctx context.Context, srcPath, vf string) (image.Image, string, error) {
	tmpDir, err := os.MkdirTemp("", "memable-decode-")
	if err != nil {
		return nil, "", fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 单文件最多等待 60 秒
	ffCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	tmpPNG := filepath.Join(tmpDir, "decoded.png")

	args := []string{"-v", "error", "-y", "-i", srcPath}
	if vf != "" {
		args = append(args, "-vf", vf)
	}
	args = append(args, "-frames:v", "1", tmpPNG)
	ffCmd := exec.CommandContext(ffCtx, "ffmpeg", args...)
	if out, err := ffCmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("FFmpeg 转换 %q: %w\n%s", srcPath, err, string(out))
	}

	f, err := os.Open(tmpPNG)
	if err != nil {
		return nil, "", fmt.Errorf("打开临时 PNG: %w", err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("解码临时 PNG: %w", err)
	}
	return img, "png", nil
}

// ImagePerceptualHashesFromImage 从已解码的 image.Image 计算感知哈希，避免重复解码。
func ImagePerceptualHashesFromImage(img image.Image) *ImageHashes {
	return &ImageHashes{
		AHash: aHash(img),
		DHash: dHash(img),
		PHash: pHash(img),
	}
}

// GenerateThumbnailFromImage 从已解码的 image.Image 生成缩略图，避免重复解码。
// 输出 JPEG（quality 90）：文件体积远小于 PNG 无损编码（约 1/6~1/8），视觉接近无损。
func GenerateThumbnailFromImage(img image.Image, outPath string, maxEdge int) error {
	if maxEdge <= 0 {
		maxEdge = 400
	}
	thumb := resizeImage(img, maxEdge)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("创建缩略图目录: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建缩略图文件 %q: %w", outPath, err)
	}
	defer f.Close()
	// JPEG 无透明通道：含透明像素的图先合成到白底，避免透明区域编码后变黑。
	if err := jpeg.Encode(f, compositeWhiteBackground(thumb), &jpeg.Options{Quality: 90}); err != nil {
		return fmt.Errorf("编码缩略图: %w", err)
	}
	return nil
}

// compositeWhiteBackground 把含透明像素的图片合成到白底后返回。
// 完全不透明的图片原样返回（不额外分配拷贝）。
func compositeWhiteBackground(img image.Image) image.Image {
	b := img.Bounds()
	opaque := true
check:
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a < 0xFFFF {
				opaque = false
				break check
			}
		}
	}
	if opaque {
		return img
	}
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}

// exifInfo 用于后续阶段读取 EXIF Orientation。
type exifInfo struct {
	Orientation int
	Width       int
	Height      int
}

// applyOrientation 根据 EXIF Orientation 旋转/翻转图片后返回新 image。
// 目前为占位实现，后续引入 EXIF 库后替换。
func applyOrientation(img image.Image, orient int) image.Image {
	if orient <= 1 || orient > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.NRGBA
	switch orient {
	case 2:
		// 水平翻转
		dst = image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+w-1-x, b.Min.Y+y).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	case 3:
		// 180 度旋转
		dst = image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+w-1-x, b.Min.Y+h-1-y).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	case 4:
		dst = image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+h-1-y).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	case 5:
		dst = image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+y, b.Min.Y+w-1-x).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	case 6:
		dst = image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+y, b.Min.Y+w-1-x).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	case 7:
		dst = image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+h-1-y, b.Min.Y+x).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	case 8:
		dst = image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+h-1-y, b.Min.Y+x).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8),
				})
			}
		}
	}
	return dst
}
