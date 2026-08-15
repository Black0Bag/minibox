// Package eventbus 提供进程内类型安全事件总线。
// 设计参考：daveamit 2026《Designing a Modular Monolith in Go》
// 模块间不直接 import，通过事件总线解耦；同步、panic 恢复。
package eventbus

import (
	"context"
	"log/slog"
	"sync"
)

// Bus 是类型安全的事件总线。
// T 是事件类型，订阅者收到 T 类型的值。
type Bus[T any] struct {
	mu     sync.RWMutex
	subs   []subEntry[T]
	nextID uint64
	logger *slog.Logger
}

// Subscriber 订阅者回调。
type Subscriber[T any] func(ctx context.Context, event T)

// subEntry 订阅条目（带唯一 token，退订可靠）。
type subEntry[T any] struct {
	id uint64
	fn Subscriber[T]
}

// New 创建事件总线。
func New[T any](logger *slog.Logger) *Bus[T] {
	return &Bus[T]{
		subs:   make([]subEntry[T], 0),
		logger: logger,
	}
}

// Subscribe 注册订阅者。返回取消函数。
func (b *Bus[T]) Subscribe(fn Subscriber[T]) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	b.subs = append(b.subs, subEntry[T]{id: id, fn: fn})
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s.id == id {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				return
			}
		}
	}
}

// Publish 发布事件。同步调用所有订阅者。
// 单个订阅者 panic 会被恢复，不影响其他订阅者。
func (b *Bus[T]) Publish(ctx context.Context, event T) {
	b.mu.RLock()
	subs := make([]Subscriber[T], len(b.subs))
	for i, s := range b.subs {
		subs[i] = s.fn
	}
	b.mu.RUnlock()

	for _, sub := range subs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					if b.logger != nil {
						b.logger.Error("事件订阅者 panic", "recover", r)
					}
				}
			}()
			sub(ctx, event)
		}()
	}
}

// SubscriberCount 返回当前订阅者数量。
func (b *Bus[T]) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
