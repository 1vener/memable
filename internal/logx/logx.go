// 包 logx：基于标准库 log/slog 的结构化日志初始化。
// 代码注释使用中文。
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Init 初始化全局 slog 默认 logger。
// level: debug/info/warn/error；format: text/json。
// filePath 非空时日志追加写入该文件，否则输出到标准输出。
func Init(level, format, filePath string) error {
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
	var w io.Writer = os.Stdout
	if strings.TrimSpace(filePath) != "" {
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("打开日志文件 %s: %w", filePath, err)
		}
		w = f
	}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	slog.SetDefault(slog.New(h))
	return nil
}
