// config_test.go：缩略图根目录与数据库路径解析测试。
// 代码注释使用中文。
package config

import (
	"os"
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

	// 未配置 → 系统数据目录（memable/thumbnails/{kind}）
	d := &Config{}
	img := d.ImageThumbDir()
	vid := d.VideoThumbDir()
	if !strings.Contains(img, filepath.Join("memable", "thumbnails", "image")) {
		t.Fatalf("默认图片目录应为系统数据目录下的 memable/thumbnails/image，实际 %q", img)
	}
	if !strings.Contains(vid, filepath.Join("memable", "thumbnails", "video")) {
		t.Fatalf("默认视频目录应为系统数据目录下的 memable/thumbnails/video，实际 %q", vid)
	}
	// 数据目录（非缓存目录）路径特征
	if strings.Contains(img, "Caches") || strings.Contains(img, "cache") {
		t.Fatalf("缩略图默认目录不应是系统缓存目录，实际 %q", img)
	}
}

// TestLoadDatabasePathDefault 验证 database.path 三态：
// 显式配置优先；未配置且工作目录有存量 memable.db 时沿用；否则系统数据目录。
func TestLoadDatabasePathDefault(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	writeCfg := func(t *testing.T, content string) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// chdirWorkDir 切换到指定工作目录并注册恢复，避免测试结束清理 TempDir 时
	// 进程 cwd 仍占用该目录（Windows 无法删除被占用目录）。
	chdirWorkDir := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })
	}

	t.Run("显式配置优先", func(t *testing.T) {
		p := writeCfg(t, "database:\n  path: D:/custom/memable.db\n")
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Database.Path != "D:/custom/memable.db" {
			t.Fatalf("应使用显式配置，实际 %q", cfg.Database.Path)
		}
	})

	t.Run("未配置且无存量库用系统数据目录", func(t *testing.T) {
		chdirWorkDir(t, t.TempDir())
		p := writeCfg(t, "")
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !strings.Contains(cfg.Database.Path, filepath.Join("memable", "memable.db")) {
			t.Fatalf("默认数据库应为系统数据目录，实际 %q", cfg.Database.Path)
		}
	})

	t.Run("未配置且工作目录有存量库也用系统目录", func(t *testing.T) {
		dir := t.TempDir()
		chdirWorkDir(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "memable.db"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		p := writeCfg(t, "")
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !strings.Contains(cfg.Database.Path, filepath.Join("memable", "memable.db")) {
			t.Fatalf("未显式配置时即使有存量库也应使用系统数据目录，实际 %q", cfg.Database.Path)
		}
	})

	t.Run("配置为空串回退系统目录", func(t *testing.T) {
		chdirWorkDir(t, t.TempDir())
		p := writeCfg(t, "database:\n  path: \"\"\n")
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !strings.Contains(cfg.Database.Path, filepath.Join("memable", "memable.db")) {
			t.Fatalf("空串配置应回退系统数据目录，实际 %q", cfg.Database.Path)
		}
	})
}
