package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config 日志配置（对应 T-10：slog + lumberjack 两层方案）
type Config struct {
	Level      string `yaml:"level"`        // debug/info/warn/error
	Format     string `yaml:"format"`       // json / text
	File       string `yaml:"file"`         // 日志文件路径；空 = 仅标准输出
	MaxSize    int    `yaml:"max_size_mb"`  // 单文件大小上限（MB）
	MaxBackups int    `yaml:"max_backups"`  // 保留旧文件数
	MaxAge     int    `yaml:"max_age_days"` // 保留天数
	Compress   bool   `yaml:"compress"`     // 旧文件是否 gzip 压缩
	BufferSize int    `yaml:"buffer_size"`  // 内存环形缓冲条目数（供 /api/logs 查询）
}

// Entry 内存日志条目（供日志查询 API 使用）
type Entry struct {
	Time    time.Time   `json:"time"`
	Level   string      `json:"level"`
	Message string      `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Default 默认配置
func Default() Config {
	return Config{
		Level:      "info",
		Format:     "text",
		File:       "",
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
		BufferSize: 5000,
	}
}

// ParseLevel 解析字符串级别（大小写不敏感）
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	case "":
		return slog.LevelInfo, nil
	default:
		return slog.LevelInfo, fmt.Errorf("未知日志级别 %q（可选：debug/info/warn/error）", s)
	}
}

var levelVar slog.LevelVar

// memoryBuffer 线程安全的内存环形缓冲
type memoryBuffer struct {
	mu      sync.RWMutex
	entries []Entry
	cap     int
}

func (b *memoryBuffer) add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, e)
	if len(b.entries) > b.cap {
		b.entries = b.entries[len(b.entries)-b.cap:]
	}
}

// Query 按级别/数量/时间筛选，返回按时间正序
func (b *memoryBuffer) Query(minLevel slog.Level, limit int, since time.Time) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	var out []Entry
	for i := len(b.entries) - 1; i >= 0; i-- {
		e := b.entries[i]
		lv, _ := ParseLevel(e.Level)
		if lv < minLevel {
			continue
		}
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	// 反转为时间正序
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// bufferHandler 将日志同时写入内存缓冲与下游 handler
type bufferHandler struct {
	next slog.Handler
	buf  *memoryBuffer
}

func (h *bufferHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *bufferHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.buf.add(Entry{Time: r.Time, Level: r.Level.String(), Message: r.Message, Attrs: attrs})
	return h.next.Handle(ctx, r)
}

func (h *bufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bufferHandler{next: h.next.WithAttrs(attrs), buf: h.buf}
}

func (h *bufferHandler) WithGroup(name string) slog.Handler {
	return &bufferHandler{next: h.next.WithGroup(name), buf: h.buf}
}

var (
	mu        sync.RWMutex
	buf       *memoryBuffer
	initErr   error
	initialized bool
)

// Init 初始化全局日志器，返回主 logger
func Init(cfg Config) (*slog.Logger, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 5000
	}

	levelVar.Set(level)

	var out io.Writer = os.Stdout
	if cfg.File != "" {
		lj := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
		}
		if lj.MaxSize <= 0 {
			lj.MaxSize = 10
		}
		out = lj
	}

	opts := &slog.HandlerOptions{Level: &levelVar}
	var base slog.Handler
	if cfg.Format == "json" {
		base = slog.NewJSONHandler(out, opts)
	} else {
		base = slog.NewTextHandler(out, opts)
	}

	mu.Lock()
	defer mu.Unlock()
	buf = &memoryBuffer{cap: cfg.BufferSize}
	handler := &bufferHandler{next: base, buf: buf}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	initialized = true
	return logger, nil
}

// Query 查询内存日志（级别用字符串）
func Query(level string, limit int, since time.Time) ([]Entry, error) {
	minLevel, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	mu.RLock()
	b := buf
	mu.RUnlock()
	if b == nil {
		return []Entry{}, nil
	}
	return b.Query(minLevel, limit, since), nil
}

// SetLevel 动态切换日志级别（返回旧级别）
func SetLevel(level string) (string, error) {
	lv, err := ParseLevel(level)
	if err != nil {
		return "", err
	}
	old := strings.ToLower(levelVar.Level().String())
	levelVar.Set(lv)
	slog.Info("日志级别已切换", "from", old, "to", level)
	return old, nil
}

// CurrentLevel 当前级别（小写，与 ParseLevel 输入格式一致）
func CurrentLevel() string {
	return strings.ToLower(levelVar.Level().String())
}
