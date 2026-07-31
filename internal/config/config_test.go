// config_test.go：缩略图根目录解析测试（配置优先，否则系统推荐目录）。
// 代码注释使用中文。
package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestThumbDirResolution(t *testing.T) {
	// 配置了 image_dir/video_dir → 使用配置值
	c := &Config{Thumbnail: ThumbnailConfig{ImageDir: "D:/my/thumbs/img", VideoDir: "./thumbs/video"}}
	if got := c.ImageThumbDir(); got != filepath.Clean("D:/my/thumbs/img") {
		t.Fatalf("ImageThumbDir 应使用配置值，实际 %q", got)
	}
	if got := c.VideoThumbDir(); got != filepath.Clean("./thumbs/video") {
		t.Fatalf("VideoThumbDir 应使用配置值，实际 %q", got)
	}

	// 未配置 → 系统推荐目录（memable/thumbnails/{kind}）
	d := &Config{}
	img := d.ImageThumbDir()
	vid := d.VideoThumbDir()
	if !strings.Contains(img, filepath.Join("memable", "thumbnails", "image")) {
		t.Fatalf("默认图片目录应为系统缓存下的 memable/thumbnails/image，实际 %q", img)
	}
	if !strings.Contains(vid, filepath.Join("memable", "thumbnails", "video")) {
		t.Fatalf("默认视频目录应为系统缓存下的 memable/thumbnails/video，实际 %q", vid)
	}
}
