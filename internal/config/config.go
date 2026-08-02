// 包 config：配置文件加载（config.yaml + Viper）。
// 代码注释使用中文。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	File   string `mapstructure:"file"`   // 可选：日志文件路径；空=输出到标准输出
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
// 否则使用系统数据目录（Windows %LOCALAPPDATA%、macOS ~/Library/Application Support、
// Linux $XDG_DATA_HOME 或 ~/.local/share 下的 memable/thumbnails/image）。
func (c *Config) ImageThumbDir() string {
	return thumbRootDir(c.Thumbnail.ImageDir, "image")
}

// VideoThumbDir 返回视频封面根目录（规则同上，video 子目录）。
func (c *Config) VideoThumbDir() string {
	return thumbRootDir(c.Thumbnail.VideoDir, "video")
}

// dataRootDir 返回系统数据目录（数据库、缩略图等持久数据的推荐位置）：
// Windows %LOCALAPPDATA%、macOS ~/Library/Application Support、
// Linux $XDG_DATA_HOME（未设则 ~/.local/share）；全部不可用时回退临时目录。
func dataRootDir() string {
	var root string
	switch runtime.GOOS {
	case "windows":
		root = os.Getenv("LOCALAPPDATA")
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, "Library", "Application Support")
		}
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			root = d
		} else if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, ".local", "share")
		}
	}
	if root == "" {
		root = os.TempDir()
	}
	return root
}

// defaultDataDBPath 返回数据库默认路径（系统数据目录下的 memable/memable.db）。
func defaultDataDBPath() string {
	return filepath.Join(dataRootDir(), "memable", "memable.db")
}

// thumbRootDir 解析缩略图根目录：配置优先，否则用系统数据目录。
func thumbRootDir(configured, kind string) string {
	if strings.TrimSpace(configured) != "" {
		return filepath.Clean(configured)
	}
	return filepath.Join(dataRootDir(), "memable", "thumbnails", kind)
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
	// PoolSize 并发数。按磁盘类型调整：SSD/NVMe 建议 16-32（高队列深度吃满带宽），
	// 机械硬盘 4-8（并发过高反而寻道抖动降低吞吐）。
	PoolSize int `mapstructure:"pool_size"`
}

// Load 读取 config.yaml；env 前缀 MEMABLE_，如 MEMABLE_DATABASE__PATH。
func Load(path string) (*Config, error) {
	v := viper.New()

	// 默认值
	v.SetDefault("database.path", "memable.db")
	v.SetDefault("thumbnail.image_dir", "")
	v.SetDefault("thumbnail.video_dir", "")
	v.SetDefault("thumbnail.max_edge", 400)
	v.SetDefault("video.sprite_frames", 25)
	v.SetDefault("similarity.image_phash_distance", 10)
	v.SetDefault("similarity.image_dhash_distance", 10)
	v.SetDefault("similarity.image_ahash_distance", 10)
	v.SetDefault("similarity.video_phash_distance", 12)
	v.SetDefault("similarity.video_duration_diff_ms", 3000)
	v.SetDefault("worker.pool_size", 8)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("delete.permanent", false)
	v.SetDefault("ui.default_page_size", 20)
	v.SetDefault("ui.page_size_options", []int{10, 20, 50, 100})

	v.SetConfigFile(path)
	v.SetEnvPrefix("MEMABLE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

	// 文件不存在时允许用默认值+env 启动；但解析错误（YAML 语法、编码等）必须暴露，
	// 否则所有配置会静默回退到 SetDefault 默认值，导致 delete.permanent 等关键项与用户预期不符。
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 找不到默认文件允许忽略；显式路径不同则不进入此分支
		} else {
			return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
		}
	}

	// database.path 默认两态：
	// 1) 显式配置（yaml 写了 database.path 或 env MEMABLE_DATABASE__PATH，非空）→ 按配置；
	// 2) 否则 → 系统数据目录（dataRootDir()/memable/memable.db）。
	// 注意：viper 的 IsSet 会把 SetDefault 的默认值也算作已设置，不能用于显式判定；
	// 不再做"工作目录存量 memable.db 沿用"兼容——注释掉配置即迁移到系统目录，
	// 存量文件由用户手动迁移。
	explicit := v.InConfig("database.path") || os.Getenv("MEMABLE_DATABASE__PATH") != ""
	if !explicit || strings.TrimSpace(v.GetString("database.path")) == "" {
		v.Set("database.path", defaultDataDBPath())
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
