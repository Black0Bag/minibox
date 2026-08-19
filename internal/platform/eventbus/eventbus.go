// Package eventbus 提供进程内事件总线（类型化发布/订阅）。
//
// 设计依据：
//   - dev/02_后端开发路线图.md：platform/eventbus 进程内事件总线
//   - 解耦模块间通知：session hub / degradation / backup / upgrade 等模块通过事件总线通信
//
// 关键实践（互联网 2026 校准）：
//   - 同步派发（Publish 阻塞直到所有 handler 执行完毕，保证事件顺序）
//   - handler panic 隔离（一个 handler panic 不影响其他 handler 和发布者）
//   - 泛型类型化（Go 1.18+ 泛型，编译期类型安全）
//   - 无 buffer channel 避免内存泄漏（同步派发不需要 channel）
//   - Unsubscribe 支持取消订阅（防 goroutine 泄漏）
package eventbus

import (
	"log/slog"
	"sync"
)

// Handler 事件处理函数。
type Handler[T any] func(event T)

// EventBus 类型化进程内事件总线。
// 同步派发：Publish 阻塞直到所有 handler 执行完毕。
type EventBus[T any] struct {
	mu       sync.RWMutex
	handlers map[uint64]Handler[T]
	nextID   uint64
	logger   *slog.Logger
}

// New 创建事件总线。
func New[T any](logger *slog.Logger) *EventBus[T] {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventBus[T]{
		handlers: make(map[uint64]Handler[T]),
		logger:   logger,
	}
}

// Subscribe 订阅事件，返回取消订阅函数。
func (b *EventBus[T]) Subscribe(h Handler[T]) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	b.handlers[id] = h
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.handlers, id)
	}
}

// Publish 同步派发事件给所有订阅者。
// handler panic 被隔离（不影响其他 handler 和发布者）。
func (b *EventBus[T]) Publish(event T) {
	b.mu.RLock()
	handlers := make([]Handler[T], 0, len(b.handlers))
	for _, h := range b.handlers {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		b.safeCall(h, event)
	}
}

// safeCall 安全调用 handler（panic 隔离）。
func (b *EventBus[T]) safeCall(h Handler[T], event T) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("事件 handler panic", "panic", r)
		}
	}()
	h(event)
}

// SubscriberCount 返回当前订阅者数量（调试/监控用）。
func (b *EventBus[T]) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.handlers)
}