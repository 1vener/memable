// 包 main：服务启动入口。
// 代码注释使用中文。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"memable/internal/api"
	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/duplicate"
	"memable/internal/logx"
	"memable/internal/repo"
	"memable/internal/scan"
	"memable/internal/search"
	"memable/internal/task"
)

// version 构建时通过 -ldflags "-X main.version=..." 注入（Build-Release.ps1 / CI）。
var version = "dev"

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径")
	port := flag.Int("port", 0, "HTTP 监听端口（覆盖配置 server.port；0=不覆盖，默认 12358）")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}
	if *port > 0 {
		cfg.Server.Port = *port
	}
	if err := logx.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.File); err != nil {
		slog.Error("初始化日志失败", "err", err)
		os.Exit(1)
	}

	dbh, err := db.Open(cfg)
	if err != nil {
		slog.Error("打开数据库失败", "err", err, "path", cfg.Database.Path)
		os.Exit(1)
	}
	defer dbh.Close()

	if err := db.Migrate(dbh); err != nil {
		slog.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}

	v, _ := db.SchemaVersion(dbh)
	slog.Info("数据库就绪",
		"schema_version", v,
		"db", cfg.Database.Path,
	)

	// 初始化各层 Repository
	libRepo := repo.NewLibraryRepo(dbh)
	sessionRepo := repo.NewSessionRepo(dbh)
	mediaRepo := repo.NewMediaRepo(dbh)
	taskRepo := repo.NewTaskRepo(dbh)
	fileStatsRepo := repo.NewFileStatsRepo(dbh)
	dupRepo := repo.NewDuplicateRepo(dbh)

	// 初始化服务层：缩略图根目录按类型解析（配置优先，否则系统推荐目录）
	imageThumbBase := cfg.ImageThumbDir()
	videoThumbBase := cfg.VideoThumbDir()

	scanSvc := &scan.Service{
		Sessions:       sessionRepo,
		Media:          mediaRepo,
		Config:         cfg,
		ImageThumbBase: imageThumbBase,
		VideoThumbBase: videoThumbBase,
		Libraries:      libRepo,
	}
	searchSvc := search.NewService(mediaRepo, libRepo)
	dupSvc := duplicate.NewService(dupRepo, repo.NewDirDuplicateRepo(dbh), mediaRepo, libRepo, cfg, imageThumbBase, videoThumbBase)

	// 初始化任务调度器
	settingsRepo := repo.NewSettingsRepo(dbh)
	runner := task.NewRunner(taskRepo, sessionRepo, mediaRepo, libRepo, settingsRepo, scanSvc, task.RunnerConfig{
		PoolSize:                  cfg.Worker.PoolSize,
		ImageThumbBase:            imageThumbBase,
		VideoThumbBase:            videoThumbBase,
		PermanentDelete:           cfg.Delete.Permanent,
		NetdriveRequestIntervalMs: cfg.Netdrive.RequestIntervalMs,
		NetdriveAddress:           cfg.Netdrive.CD2Address,
	}, dupSvc)
	runner.Start(context.Background())

	// 启动 HTTP API 服务器
	srv := api.NewServer(cfg, libRepo, sessionRepo, mediaRepo, taskRepo, fileStatsRepo, settingsRepo, scanSvc, searchSvc, runner, imageThumbBase, videoThumbBase, dupSvc)

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("收到退出信号，正在关闭...")
		runner.Stop()
		srv.Shutdown(context.Background())
	}()

	slog.Info("memable 服务启动", "version", version)
	if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
		slog.Error("服务器异常退出", "err", err)
		os.Exit(1)
	}
	slog.Info("服务已停止")
}
