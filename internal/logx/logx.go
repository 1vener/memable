// 包 logx：基于标准库 log/slog 的结构化日志初始化。
// 代码注释使用中文。
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"memable/internal/config"
)

// Init 初始化全局 slog 默认 logger。
// level: debug/info/warn/error；format: text/json。
// filePath 非空时日志追加写入该文件，否则输出到标准输出；
// windowsgui 发布构建（无控制台，stdout 不可写）且未配置日志文件时，
// 自动回退写入系统数据目录 memable/server.log。
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
	} else if !stdoutAvailable() {
		// GUI 程序（-H windowsgui）没有控制台：os.Stdout 是无效句柄，
		// 日志会静默丢失，自动回退到系统数据目录的日志文件。
		dir := filepath.Join(config.DataRootDir(), "memable")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			filePath = filepath.Join(dir, "server.log")
		}
		if f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			w = f
		}
	}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	slog.SetDefault(slog.New(h))
	if filePath != "" {
		slog.Info("日志写入文件", "file", filePath)
	}
	return nil
}

// stdoutAvailable 检测标准输出是否可用（无控制台时 Stat 会失败）。
func stdoutAvailable() bool {
	_, err := os.Stdout.Stat()
	return err == nil
}
