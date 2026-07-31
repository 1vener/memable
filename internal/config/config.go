// 包 config：配置文件加载（config.yaml + Viper）。
// 代码注释使用中文。
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config 聚合全部运行配置（不入库，来自 config.yaml）。
type Config struct {
	Database   DatabaseConfig   `mapstructure:"database"`
	Thumbnail  ThumbnailConfig  `mapstructure:"thumbnail"`
	Video      VideoConfig      `mapstructure:"video"`
	Similarity SimilarityConfig `mapstructure:"similarity"`
	Worker     WorkerConfig     `mapstructure:"worker"`
	Log        LogConfig        `mapstructure:"log"`
	Delete     DeleteConfig     `mapstructure:"delete"`
	UI         UIConfig         `mapstructure:"ui"`
}

// DeleteConfig 删除安全策略。
type DeleteConfig struct {
	Permanent bool `mapstructure:"permanent"` // true=永久删除；false=默认移入系统回收站
}

// UIConfig 客户端 UI 默认值。
type UIConfig struct {
	DefaultPageSize int   `mapstructure:"default_page_size"`
	PageSizeOptions []int `mapstructure:"page_size_options"`
}

// LogConfig 结构化日志配置。
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug/info/warn/error
	Format string `mapstructure:"format"` // text/json
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"` // SQLite 文件路径，":memory:" 表示内存库
}

type ThumbnailConfig struct {
	ImageDir string `mapstructure:"image_dir"` // 图片缩略图目录
	VideoDir string `mapstructure:"video_dir"` // 视频帧/封面目录
	MaxEdge  int    `mapstructure:"max_edge"`  // 缩略图最大边
}

// ImageThumbDir 返回图片缩略图根目录：配置了 image_dir 则用配置值，
// 否则使用系统推荐目录（Windows %LOCALAPPDATA%、macOS ~/Library/Caches、
// Linux $XDG_CACHE_HOME 或 ~/.cache 下的 memable/thumbnails/image）。
func (c *Config) ImageThumbDir() string {
	return thumbRootDir(c.Thumbnail.ImageDir, "image")
}

// VideoThumbDir 返回视频封面根目录（规则同上，video 子目录）。
func (c *Config) VideoThumbDir() string {
	return thumbRootDir(c.Thumbnail.VideoDir, "video")
}

// thumbRootDir 解析缩略图根目录：配置优先，否则用系统缓存目录。
func thumbRootDir(configured, kind string) string {
	if strings.TrimSpace(configured) != "" {
		return filepath.Clean(configured)
	}
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "memable", "thumbnails", kind)
}

type VideoConfig struct {
	SpriteFrames int `mapstructure:"sprite_frames"` // sprite 临时截图数量，固定建议 25
}

type SimilarityConfig struct {
	ImagePHashDistance  int   `mapstructure:"image_phash_distance"`   // 图片 pHash 最大 Hamming 距离
	ImageDHashDistance  int   `mapstructure:"image_dhash_distance"`   // 图片 dHash 最大 Hamming 距离
	ImageAHashDistance  int   `mapstructure:"image_ahash_distance"`   // 图片 aHash 最大 Hamming 距离
	VideoPHashDistance  int   `mapstructure:"video_phash_distance"`   // 视频 sprite pHash 最大 Hamming 距离
	VideoDurationDiffMs int64 `mapstructure:"video_duration_diff_ms"` // 视频允许时长差（毫秒）
}

type WorkerConfig struct {
	PoolSize int `mapstructure:"pool_size"` // Worker Pool 并发数
}

// Load 读取 config.yaml；env 前缀 MEMABLE_，如 MEMABLE_DATABASE__PATH。
func Load(path string) (*Config, error) {
	v := viper.New()

	// 默认值
	v.SetDefault("database.path", "memable.db")
	v.SetDefault("thumbnail.image_dir", "")
	v.SetDefault("thumbnail.video_dir", "")
	v.SetDefault("thumbnail.max_edge", 300)
	v.SetDefault("video.sprite_frames", 25)
	v.SetDefault("similarity.image_phash_distance", 10)
	v.SetDefault("similarity.image_dhash_distance", 10)
	v.SetDefault("similarity.image_ahash_distance", 10)
	v.SetDefault("similarity.video_phash_distance", 12)
	v.SetDefault("similarity.video_duration_diff_ms", 3000)
	v.SetDefault("worker.pool_size", 4)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("delete.permanent", false)
	v.SetDefault("ui.default_page_size", 20)
	v.SetDefault("ui.page_size_options", []int{10, 20, 50, 100})

	v.SetConfigFile(path)
	v.SetEnvPrefix("MEMABLE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

	// 文件不存在时仍允许用默认值+env 启动（但显式要求路径时应当可见错误）
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 找不到默认文件允许忽略；显式路径不同则不进入此分支
		}
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
