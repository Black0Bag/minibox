package logging

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
)

func TestNew(t *testing.T) {
	cfg := config.Default()
	cfg.Logging.Format = "text"
	cfg.Logging.Output = "stdout"

	l, err := New(cfg.Logging)
	if err != nil {
		t.Fatalf("New 错误: %v", err)
	}
	if l == nil {
		t.Fatal("logger 为 nil")
	}
	_ = l
}

func TestHandlerLevel(t *testing.T) {
	// 验证 Info 级能通过 Enabled 检查
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	if !h.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("Info 级应被启用")
	}
	if h.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("Debug 级应被禁用（默认 Info）")
	}
}
