package eventbus

import (
	"sync/atomic"
	"testing"
)

type TestEvent struct {
	Msg string
}

func TestSubscribeAndPublish(t *testing.T) {
	bus := New[TestEvent](nil)
	var received TestEvent
	unsub := bus.Subscribe(func(e TestEvent) {
		received = e
	})
	defer unsub()

	bus.Publish(TestEvent{Msg: "hello"})

	if received.Msg != "hello" {
		t.Errorf("received = %v, want hello", received)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := New[TestEvent](nil)
	var count atomic.Int32
	unsub1 := bus.Subscribe(func(e TestEvent) { count.Add(1) })
	unsub2 := bus.Subscribe(func(e TestEvent) { count.Add(1) })
	defer unsub1()
	defer unsub2()

	bus.Publish(TestEvent{})

	if count.Load() != 2 {
		t.Errorf("count = %d, want 2", count.Load())
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := New[TestEvent](nil)
	var count atomic.Int32
	unsub := bus.Subscribe(func(e TestEvent) { count.Add(1) })

	bus.Publish(TestEvent{})
	if count.Load() != 1 {
		t.Fatalf("count = %d, want 1", count.Load())
	}

	unsub()
	bus.Publish(TestEvent{})
	if count.Load() != 1 {
		t.Errorf("unsubscribe 后 count = %d, want 1", count.Load())
	}
}

func TestPanicIsolation(t *testing.T) {
	bus := New[TestEvent](nil)
	var received bool
	bus.Subscribe(func(e TestEvent) {
		panic("handler panic")
	})
	bus.Subscribe(func(e TestEvent) {
		received = true
	})

	bus.Publish(TestEvent{})

	if !received {
		t.Error("panic handler 应不影响后续 handler")
	}
}

func TestNoSubscribers(t *testing.T) {
	bus := New[TestEvent](nil)
	// 无订阅者时 Publish 不应 panic
	bus.Publish(TestEvent{Msg: "nobody listening"})
}

func TestSubscriberCount(t *testing.T) {
	bus := New[TestEvent](nil)
	if bus.SubscriberCount() != 0 {
		t.Errorf("初始 count = %d, want 0", bus.SubscriberCount())
	}
	unsub := bus.Subscribe(func(e TestEvent) {})
	if bus.SubscriberCount() != 1 {
		t.Errorf("count = %d, want 1", bus.SubscriberCount())
	}
	unsub()
	if bus.SubscriberCount() != 0 {
		t.Errorf("count = %d, want 0", bus.SubscriberCount())
	}
}

func TestConcurrentAccess(t *testing.T) {
	bus := New[TestEvent](nil)
	var count atomic.Int32

	// 并发订阅 + 发布
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			unsub := bus.Subscribe(func(e TestEvent) { count.Add(1) })
			bus.Publish(TestEvent{})
			unsub()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	// 不 panic 即通过
}