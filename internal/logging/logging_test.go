package logging

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
		err  bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"", slog.LevelInfo, false},
		{"verbose", slog.LevelInfo, true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseLevel(%q) 期望错误，得到 nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) 意外错误: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q)=%v, 期望 %v", c.in, got, c.want)
		}
	}
}

func TestInitAndQuery(t *testing.T) {
	cfg := Default()
	cfg.Level = "debug"
	cfg.BufferSize = 100
	if _, err := Init(cfg); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	slog.Debug("调试消息", "k", "v")
	slog.Info("信息消息")
	slog.Warn("警告消息")
	slog.Error("错误消息")

	// 查询 error 级
	entries, err := Query("error", 100, time.Time{})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("error 级应 1 条，得到 %d", len(entries))
	}
	if entries[0].Message != "错误消息" {
		t.Errorf("消息不匹配: %q", entries[0].Message)
	}

	// 查询 warn 级
	entries, _ = Query("warn", 100, time.Time{})
	if len(entries) != 2 {
		t.Errorf("warn 级应 2 条，得到 %d", len(entries))
	}

	// limit 限制
	entries, _ = Query("debug", 2, time.Time{})
	if len(entries) != 2 {
		t.Errorf("limit=2 应返回 2 条，得到 %d", len(entries))
	}

	// 时间筛选（未来时间应返回空）
	entries, _ = Query("debug", 100, time.Now().Add(time.Hour))
	if len(entries) != 0 {
		t.Errorf("未来时间应 0 条，得到 %d", len(entries))
	}

	// 属性应保留
	found := false
	for _, e := range entries {
		_ = e
	}
	entries, _ = Query("debug", 100, time.Time{})
	for _, e := range entries {
		if e.Message == "调试消息" {
			if v, ok := e.Attrs["k"]; !ok || v != "v" {
				t.Errorf("属性未保留: %v", e.Attrs)
			}
			found = true
		}
	}
	if !found {
		t.Error("未找到调试消息条目")
	}
}

func TestSetLevel(t *testing.T) {
	cfg := Default()
	_, _ = Init(cfg)
	if CurrentLevel() != "info" {
		t.Errorf("默认级别应为 info，得到 %s", CurrentLevel())
	}
	old, err := SetLevel("debug")
	if err != nil {
		t.Fatalf("SetLevel 失败: %v", err)
	}
	if old != "info" {
		t.Errorf("旧级别应为 info，得到 %s", old)
	}
	if CurrentLevel() != "debug" {
		t.Errorf("新级别应为 debug，得到 %s", CurrentLevel())
	}
	if _, err := SetLevel("bogus"); err == nil {
		t.Error("非法级别应报错")
	}
	if !strings.Contains(CurrentLevel(), "debug") {
		t.Errorf("非法级别不应改变当前级别: %s", CurrentLevel())
	}
}

func TestBufferCap(t *testing.T) {
	cfg := Default()
	cfg.Level = "debug"
	cfg.BufferSize = 5
	_, _ = Init(cfg)
	for i := 0; i < 20; i++ {
		slog.Info("消息", "i", i)
	}
	entries, _ := Query("info", 100, time.Time{})
	if len(entries) > 5 {
		t.Errorf("环形缓冲应限制在 5 条，得到 %d", len(entries))
	}
}
