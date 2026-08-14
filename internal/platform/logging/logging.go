// Package logging 提供 slog 日志封装（text/json 格式 + 文件轮转）。
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/Black0Bag/minibox/internal/config"
)

// New 根据配置创建 slog.Logger。
// 支持 text（本地开发）和 json（生产）两种格式，输出到 stdout/stderr/文件。
func New(cfg config.LoggingConfig) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	// 构建 handler options
	opts := &slog.HandlerOptions{Level: level}

	// 输出目标
	var w io.Writer
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		w = os.Stderr
	case "stdout", "":
		w = os.Stdout
	default:
		// 文件输出 + lumberjack 轮转
		w = &lumberjack.Logger{
			Filename:   cfg.Output,
			MaxSize:    cfg.MaxSizeMB,   // MB
			MaxBackups: cfg.MaxBackups,  // 保留备份数
			MaxAge:     cfg.MaxAgeDays,  // 保留天数
			LocalTime:  true,
			Compress:   true,
		}
	}

	// 选择格式
	var h slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		h = slog.NewTextHandler(w, opts)
	}

	return slog.New(h), nil
}

// parseLevel 解析日志级别字符串。
func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, nil
	}
}
