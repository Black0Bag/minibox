package eventbus

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// TestSubscribeUnsubscribe 验证订阅/退订（修复 &s==&fn 永不匹配 bug）。
func TestSubscribeUnsubscribe(t *testing.T) {
	bus := New[int](slog.Default())

	var mu sync.Mutex
	received := 0
	unsub := bus.Subscribe(func(_ context.Context, e int) {
		mu.Lock()
		defer mu.Unlock()
		received += e
	})

	bus.Publish(context.Background(), 1)
	bus.Publish(context.Background(), 2)

	mu.Lock()
	if received != 3 {
		t.Errorf("订阅后应收到 3，实际 %d", received)
	}
	mu.Unlock()

	// 退订后不再收到
	unsub()
	bus.Publish(context.Background(), 10)

	mu.Lock()
	if received != 3 {
		t.Errorf("退订后不应再收到，实际 %d（退订失效 bug）", received)
	}
	mu.Unlock()

	if bus.SubscriberCount() != 0 {
		t.Errorf("退订后订阅者数应为 0，实际 %d", bus.SubscriberCount())
	}
}

// TestPublishPanicIsolation 单个订阅者 panic 不影响其他。
func TestPublishPanicIsolation(t *testing.T) {
	bus := New[string](slog.Default())

	called := false
	bus.Subscribe(func(_ context.Context, _ string) {
		panic("订阅者崩了")
	})
	bus.Subscribe(func(_ context.Context, _ string) {
		called = true
	})

	// 不应 panic
	bus.Publish(context.Background(), "事件")
	if !called {
		t.Error("第二个订阅者应被调用（panic 隔离）")
	}
}

// TestMultipleSubscribers 多个订阅者都收到。
func TestMultipleSubscribers(t *testing.T) {
	bus := New[int](slog.Default())
	count := 0

	for i := 0; i < 5; i++ {
		bus.Subscribe(func(_ context.Context, _ int) {
			count++
		})
	}
	bus.Publish(context.Background(), 1)
	if count != 5 {
		t.Errorf("5 个订阅者都应收到，实际 %d", count)
	}
}
