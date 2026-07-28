// 包 logx：基于标准库 log/slog 的结构化日志初始化。
// 代码注释使用中文。
package logx

import (
	"log/slog"
	"os"
)

// Init 初始化全局 slog 默认 logger。
// level: debug/info/warn/error；format: text/json。
func Init(level, format string) {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
}
