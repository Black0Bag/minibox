package timestamp

import (
	"context"
	"testing"
	"time"
)

func TestFormatLayout(t *testing.T) {
	want := "2006-01-02 15:04:05"
	if Layout != want {
		t.Errorf("Layout = %q, want %q", Layout, want)
	}
}

func TestFormatTime(t *testing.T) {
	tm := time.Date(2026, 8, 19, 14, 30, 45, 0, time.UTC)
	got := FormatTime(tm)
	want := "2026-08-19 14:30:45"
	if got != want {
		t.Errorf("FormatTime = %q, want %q", got, want)
	}
}

func TestNextSeq(t *testing.T) {
	s := New(nil)
	s1 := s.NextSeq()
	s2 := s.NextSeq()
	s3 := s.NextSeq()
	if s1 != 1 {
		t.Errorf("第一个序号 = %d, want 1", s1)
	}
	if s2 != 2 {
		t.Errorf("第二个序号 = %d, want 2", s2)
	}
	if s3 != 3 {
		t.Errorf("第三个序号 = %d, want 3", s3)
	}
}

func TestNextSeqConcurrent(t *testing.T) {
	s := New(nil)
	const n = 1000
	done := make(chan uint64, n)
	for i := 0; i < n; i++ {
		go func() {
			done <- s.NextSeq()
		}()
	}
	seen := make(map[uint64]bool, n)
	for i := 0; i < n; i++ {
		seq := <-done
		if seen[seq] {
			t.Fatalf("序号重复: %d", seq)
		}
		seen[seq] = true
	}
}

func TestNowWithoutNTP(t *testing.T) {
	s := New(nil)
	before := time.Now()
	got := s.Now()
	after := time.Now()
	if got.Before(before.Add(-10 * time.Millisecond)) {
		t.Errorf("Now() 在系统时间之前太多: got %v, before %v", got, before)
	}
	if got.After(after.Add(10 * time.Millisecond)) {
		t.Errorf("Now() 在系统时间之后太多: got %v, after %v", got, after)
	}
}

func TestFormatWithoutNTP(t *testing.T) {
	s := New(nil)
	got := s.Format()
	if len(got) != 19 {
		t.Errorf("Format 长度 = %d, want 19, got %q", len(got), got)
	}
	if got[4] != '-' || got[7] != '-' || got[10] != ' ' || got[13] != ':' || got[16] != ':' {
		t.Errorf("Format 格式非法: %q", got)
	}
}

func TestNTPSyncedDefault(t *testing.T) {
	s := New(nil)
	if s.NTPSynced() {
		t.Error("未同步时 NTPSynced() 应返回 false")
	}
}

func TestOffsetDefault(t *testing.T) {
	s := New(nil)
	if s.Offset() != 0 {
		t.Errorf("未同步时 Offset() = %v, want 0", s.Offset())
	}
}

func TestStartStop(t *testing.T) {
	s := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	s.Stop()
}
