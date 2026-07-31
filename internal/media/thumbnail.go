// thumbnail.go：图片缩略图生成（最大边 300px，仅用 Go 标准库）。
// 代码注释使用中文
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"path/filepath"
)

// ThumbnailKey 生成内容寻址缩略图存储键。
// storageKey 用于文件命名和路径分片；recipe 形如 "v1-300"。
func ThumbnailKey(kind, sha1 string, maxEdge int) (storageKey string) {
	recipe := fmt.Sprintf("v1-%d", maxEdge)
	input := kind + ":" + sha1 + ":" + recipe
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// ThumbnailStoragePath 返回缩略图相对"该类型缩略图根目录"的存储路径。
// 格式：{storageKey[:2]}/{storageKey}.png
// 说明：路径不含类型前缀，类型由根目录区分（image/video 各自根目录）；
// 数据库只存该相对路径，根目录移动/更换配置后无需改库。
func ThumbnailStoragePath(storageKey string) string {
	dir := storageKey[:2]
	return filepath.ToSlash(filepath.Join(dir, storageKey+".png"))
}

// GenerateImageThumbnail 生成图片缩略图并保存到 outPath（薄封装，内部走统一解码）。
func GenerateImageThumbnail(srcPath, outPath string, maxEdge int) error {
	if maxEdge <= 0 {
		maxEdge = 300
	}
	decoded, err := DecodeImage(context.Background(), srcPath, DecoderGo)
	if err != nil {
		return fmt.Errorf("解码图片 %q: %w", srcPath, err)
	}
	return GenerateThumbnailFromImage(decoded.Image, outPath, maxEdge)
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
