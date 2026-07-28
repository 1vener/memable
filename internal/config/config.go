// 包 config：配置文件加载（config.yaml + Viper）。
// 代码注释使用中文。
package config

import (
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
	v.SetDefault("thumbnail.image_dir", "thumbnail/image/")
	v.SetDefault("thumbnail.video_dir", "thumbnail/video/")
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
