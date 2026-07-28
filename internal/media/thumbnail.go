// thumbnail.go：图片缩略图生成（最大边 300px，仅用 Go 标准库）。
// 代码注释使用中文。
package media

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
)

// GenerateImageThumbnail 生成图片缩略图并保存到 outPath。
// 最大边不超过 maxEdge；使用最近邻缩放，输出 PNG 格式。
func GenerateImageThumbnail(srcPath, outPath string, maxEdge int) error {
	if maxEdge <= 0 {
		maxEdge = 300
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("打开图片 %q: %w", srcPath, err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("解码图片 %q: %w", srcPath, err)
	}

	thumb := resizeImage(img, maxEdge)

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("创建缩略图目录: %w", err)
	}

	var buf bytes.Buffer
	if err := encodePNG(&buf, thumb); err != nil {
		return fmt.Errorf("编码缩略图: %w", err)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("写入缩略图 %q: %w", outPath, err)
	}
	return nil
}

// resizeImage 按最大边等比缩放；小于 maxEdge 则保持原尺寸。
func resizeImage(img image.Image, maxEdge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return img
	}
	var nw, nh int
	if w >= h {
		nw = maxEdge
		nh = h * maxEdge / w
	} else {
		nh = maxEdge
		nw = w * maxEdge / h
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*b.Dy()/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*b.Dx()/nw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	return dst
}

// encodePNG 将图片编码为 PNG 写入 buf。
func encodePNG(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}
