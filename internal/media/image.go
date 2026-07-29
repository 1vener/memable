// image.go：图片 metadata 采集。
// 代码注释使用中文。
package media

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// ImageMeta 图片基础 metadata。
type ImageMeta struct {
	Format string
	Width  int
	Height int
}

// ProbeImage 读取图片头部 metadata，不解码完整像素。
func ProbeImage(path string) (*ImageMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	return &ImageMeta{Format: format, Width: cfg.Width, Height: cfg.Height}, nil
}
