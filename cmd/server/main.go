// 包 main：服务启动入口。
// 代码注释使用中文。
package main

import (
	"flag"
	"log/slog"
	"os"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/logx"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		// 日志尚未初始化，直接输出 stderr
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}
	logx.Init(cfg.Log.Level, cfg.Log.Format)

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
	slog.Info("memable 服务启动完成",
		"stage", "0+1.1",
		"schema_version", v,
		"db", cfg.Database.Path,
	)
}
