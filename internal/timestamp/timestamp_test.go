package timestamp

import (
	"strings"
	"testing"
	"time"
)

func TestNowWithinReasonableRange(t *testing.T) {
	s := New("")
	before := time.Now().Add(-time.Minute)
	now := s.Now()
	after := time.Now().Add(time.Minute)
	if now.Before(before) || now.After(after) {
		t.Errorf("Now 超出合理范围: %v", now)
	}
}

func TestFormat(t *testing.T) {
	s := New("")
	tt := time.Date(2026, 8, 7, 12, 34, 56, 0, time.Local)
	got := s.Format(tt)
	if got != "2026-08-07 12:34:56" {
		t.Errorf("Format 结果错误: %q", got)
	}
	if len(got) != 19 {
		t.Errorf("时间戳应为 19 字符 YYYY-MM-DD HH:MM:SS，得到 %d", len(got))
	}
}

func TestFormatNow(t *testing.T) {
	s := New("")
	got := s.FormatNow()
	// 应为 19 字符且匹配格式
	if len(got) != 19 {
		t.Errorf("FormatNow 长度错误: %q", got)
	}
	if _, err := time.ParseInLocation(TimeFormat, got, time.Local); err != nil {
		t.Errorf("FormatNow 无法按统一格式解析: %v", err)
	}
}

func TestOffsetThreadSafe(t *testing.T) {
	s := New("")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.Now()
			_ = s.FormatNow()
		}
		done <- struct{}{}
	}()
	// 并发读不 panic 即可
	<-done
}

func TestOffsetFormatValidChars(t *testing.T) {
	s := New("")
	got := s.FormatNow()
	if strings.Contains(got, "\n") {
		t.Errorf("时间戳不应包含换行: %q", got)
	}
}
