package monitor

import (
	"os"
	"testing"
	"time"
)

// TestCollector_CollectBasics 基础采集：字段非零性 + 范围校验。
func TestCollector_CollectBasics(t *testing.T) {
	c := NewCollector()
	// 第一次：建立基线（CPU 可能返回 0）
	m1 := c.Collect()
	if m1.Timestamp.IsZero() {
		t.Fatal("Timestamp 不应为零值")
	}
	if m1.Process.Goroutines <= 0 {
		t.Errorf("Goroutines 应 > 0，实际 %d", m1.Process.Goroutines)
	}
	if m1.Process.GoVersion == "" || m1.Process.OS == "" || m1.Process.Arch == "" {
		t.Errorf("进程信息不应为空: %+v", m1.Process)
	}

	// 间隔采样（保证 jiffies 变化）
	time.Sleep(120 * time.Millisecond)
	m2 := c.Collect()

	// Linux 平台上 /proc 数据应可用
	if _, err := os.Stat("/proc/stat"); err == nil {
		if m2.System.MemoryTotalBytes == 0 {
			t.Error("Linux 平台 MemoryTotalBytes 不应为 0（/proc/meminfo 可用）")
		}
		if m2.System.CPUPercent < 0 || m2.System.CPUPercent > 100 {
			t.Errorf("CPUPercent 越界: %f", m2.System.CPUPercent)
		}
	}
	if m2.System.MemoryPercent < 0 || m2.System.MemoryPercent > 100 {
		t.Errorf("MemoryPercent 越界: %f", m2.System.MemoryPercent)
	}
	if m2.System.DiskPercent < 0 || m2.System.DiskPercent > 100 {
		t.Errorf("DiskPercent 越界: %f", m2.System.DiskPercent)
	}
}

// TestCollector_Concurrent 并发安全（race 检测配合）。
func TestCollector_Concurrent(t *testing.T) {
	c := NewCollector()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = c.Collect()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}

// TestParseUint 数字解析。
func TestParseUint(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"123", 123, true},
		{"0", 0, true},
		{"18446744073709551615", 18446744073709551615, true},
		{"", 0, false},
		{"12a", 0, false},
		{"-1", 0, false},
	}
	for _, tc := range cases {
		got, err := parseUint(tc.in)
		if tc.ok && err != nil {
			t.Errorf("parseUint(%q) 不应报错: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("parseUint(%q) 应报错", tc.in)
		}
		if tc.ok && got != tc.want {
			t.Errorf("parseUint(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}