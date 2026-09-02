package app

import (
	"sync"
	"testing"

	"github.com/Black0Bag/minibox/internal/monitor"
)

// TestHistoryRing_容量上限 验证超出 limit 时淘汰最旧样本，保留最近 limit 条。
func TestHistoryRing_容量上限(t *testing.T) {
	ring := newHistoryRing(3)
	for i := range 5 {
		ring.Push(monitor.Metrics{Process: monitor.ProcessStats{Goroutines: i}})
	}

	got := ring.Snapshot()
	if len(got) != 3 {
		t.Fatalf("Snapshot 长度 = %d, want 3", len(got))
	}
	// 旧→新顺序：最后 3 条应为 2,3,4
	for i, want := range []int{2, 3, 4} {
		if got[i].Process.Goroutines != want {
			t.Errorf("got[%d].Goroutines = %d, want %d", i, got[i].Process.Goroutines, want)
		}
	}
}

// TestHistoryRing_零值回落默认容量 验证 limit <= 0 时回落 60。
func TestHistoryRing_零值回落默认容量(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if ring := newHistoryRing(limit); ring.limit != 60 {
			t.Errorf("newHistoryRing(%d).limit = %d, want 60", limit, ring.limit)
		}
	}
}

// TestHistoryRing_空缓冲返回非nil 保证 JSON 序列化为 [] 而不是 null，
// 前端无需为空历史额外判空。
func TestHistoryRing_空缓冲返回非nil(t *testing.T) {
	if got := newHistoryRing(10).Snapshot(); got == nil {
		t.Fatal("空缓冲 Snapshot 返回 nil，应返回长度 0 的非 nil 切片")
	}
}

// TestHistoryRing_并发读写 复现修复前的 data race：
// Push 由 30s 采样 goroutine 调用，Snapshot 由 HTTP handler 调用。
// 本用例在 -race 下运行可捕获未加锁实现的竞态（CI race job 提供该证据）。
func TestHistoryRing_并发读写(t *testing.T) {
	ring := newHistoryRing(16)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 200 {
				ring.Push(monitor.Metrics{Process: monitor.ProcessStats{Goroutines: i}})
			}
		})
		wg.Go(func() {
			for range 200 {
				if got := ring.Snapshot(); len(got) > 16 {
					t.Errorf("Snapshot 长度 = %d, 超出容量上限 16", len(got))
					return
				}
			}
		})
	}
	wg.Wait()

	if got := ring.Snapshot(); len(got) != 16 {
		t.Errorf("并发结束后 Snapshot 长度 = %d, want 16", len(got))
	}
}
