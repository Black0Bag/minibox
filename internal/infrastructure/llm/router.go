package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// ProviderEntry 单个供应商的路由条目。
// 含 key 池 + 熔断器 + 健康状态。
type ProviderEntry struct {
	Provider llm.Provider
	Name     string

	breaker *gobreaker.CircuitBreaker[any]
}

// NewProviderEntry 创建供应商路由条目。
func NewProviderEntry(p llm.Provider, name string) *ProviderEntry {
	cb := gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:        "llm-" + name,
		MaxRequests: 5,                // half-open 最大请求
		Interval:    30 * time.Second, // 清除计数周期
		Timeout:     10 * time.Second, // open 到 half-open 探测间隔
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// 失败率 > 20% 且请求数 > 10 时打开（n1n.ai 实证）
			return counts.Requests > 10 && float64(counts.TotalFailures)/float64(counts.Requests) > 0.2
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Info("熔断器状态变化", "provider", name, "from", from, "to", to)
		},
	})
	return &ProviderEntry{Provider: p, Name: name, breaker: cb}
}

// Execute 带熔断执行。
func (e *ProviderEntry) Execute(_ context.Context, fn func() (any, error)) (any, error) {
	return e.breaker.Execute(func() (any, error) {
		return fn()
	})
}

// Router 多供应商路由。
// 策略：
//   - round-robin：多 key 池轮询（B4 第1条）
//   - fallback：单模型多供应商级联（B4 第2条）
type Router struct {
	entries  []*ProviderEntry
	strategy string // round-robin / fallback
	rrIdx    atomic.Int64
	logger   *slog.Logger
}

// RouterConfig 路由配置。
type RouterConfig struct {
	Strategy string // round-robin / fallback
}

// NewRouter 创建路由器。
func NewRouter(entries []*ProviderEntry, cfg RouterConfig, logger *slog.Logger) *Router {
	if cfg.Strategy == "" {
		cfg.Strategy = "fallback"
	}
	return &Router{entries: entries, strategy: cfg.Strategy, logger: logger}
}

// Complete 通过路由执行非流式生成。
func (r *Router) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	switch r.strategy {
	case "round-robin":
		return r.completeRoundRobin(ctx, req)
	default:
		return r.completeFallback(ctx, req)
	}
}

// completeRoundRobin 轮询所有供应商条目。
func (r *Router) completeRoundRobin(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if len(r.entries) == 0 {
		return nil, fmt.Errorf("无可用 LLM 供应商")
	}
	start := r.rrIdx.Add(1)
	var lastErr error
	for i := 0; i < len(r.entries); i++ {
		idx := int((start + int64(i)) % int64(len(r.entries)))
		entry := r.entries[idx]

		if entry.breaker.State() == gobreaker.StateOpen {
			continue // 跳过熔断的供应商
		}

		resp, err := r.executeEntry(ctx, entry, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		r.logger.Warn("LLM 供应商失败，尝试下一个", "provider", entry.Name, "err", err)
	}
	return nil, fmt.Errorf("所有 LLM 供应商都失败: %w", lastErr)
}

// completeFallback 级联 fallback（按配置顺序）。
func (r *Router) completeFallback(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if len(r.entries) == 0 {
		return nil, fmt.Errorf("无可用 LLM 供应商")
	}
	var lastErr error
	for _, entry := range r.entries {
		if entry.breaker.State() == gobreaker.StateOpen {
			continue
		}
		resp, err := r.executeEntry(ctx, entry, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		r.logger.Warn("LLM 供应商失败，级联到下一个", "provider", entry.Name, "err", err)
	}
	return nil, fmt.Errorf("所有 LLM 供应商都失败: %w", lastErr)
}

// executeEntry 通过单个条目执行（带熔断）。
func (r *Router) executeEntry(ctx context.Context, entry *ProviderEntry, req llm.Request) (*llm.Response, error) {
	v, err := entry.Execute(ctx, func() (any, error) {
		return entry.Provider.Complete(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	resp, ok := v.(*llm.Response)
	if !ok {
		return nil, fmt.Errorf("LLM 返回类型错误")
	}
	return resp, nil
}

// Stream 通过路由执行流式（fallback 策略）。
func (r *Router) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	// 流式暂用第一个可用供应商（流式 fallback 较复杂，Phase 迭代完善）
	for _, entry := range r.entries {
		if entry.breaker.State() == gobreaker.StateOpen {
			continue
		}
		events, err := entry.Provider.Stream(ctx, req)
		if err == nil {
			return events, nil
		}
		r.logger.Warn("LLM 流式供应商失败，尝试下一个", "provider", entry.Name, "err", err)
	}
	return nil, fmt.Errorf("所有 LLM 供应商流式都失败")
}
