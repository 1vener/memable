// image_decode.go：统一图片解码入口（Go 原生 + FFmpeg 转码）。
// 代码注释使用中文。
package media

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	_ "golang.org/x/image/bmp"
)

// DecodedImage 解码后的图片。
type DecodedImage struct {
	Image  image.Image
	Format string
}

// DecodeImage 统一图片解码入口，根据 decoder 策略调用 Go 原生或 FFmpeg 临时转换。
func DecodeImage(ctx context.Context, path string, decoder DecoderKind) (*DecodedImage, error) {
	switch decoder {
	case DecoderGo:
		decoded, err := decodeGoImage(path)
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
func decodeGoImage(path string) (*DecodedImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开图片 %q: %w", path, err)
	}
	defer f.Close()
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
	tmpDir, err := os.MkdirTemp("", "memable-decode-")
	if err != nil {
		return nil, "", fmt.Errorf("创建临时目录: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 单文件最多等待 60 秒
	ffCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	tmpPNG := filepath.Join(tmpDir, "decoded.png")

	ffCmd := exec.CommandContext(ffCtx,
		"ffmpeg",
		"-v", "error",
		"-y",
		"-i", srcPath,
		"-frames:v", "1",
		tmpPNG,
	)
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
func GenerateThumbnailFromImage(img image.Image, outPath string, maxEdge int) error {
	if maxEdge <= 0 {
		maxEdge = 300
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
	if err := png.Encode(f, thumb); err != nil {
		return fmt.Errorf("编码缩略图: %w", err)
	}
	return nil
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
