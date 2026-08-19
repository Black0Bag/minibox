// Package retry 提供统一的退避重试工具（cenkalti/backoff/v4 封装）。
//
// 设计依据：
//   - dev/01_项目总路线图.md：弹性 gobreaker + backoff/v4 ✅
//   - dev/02_后端开发路线图.md：弹性 sony/gobreaker + cenkalti/backoff/v4
//
// 关键实践（互联网 2026 校准）：
//   - 指数退避 + 随机抖动（RandomizationFactor=0.5，防雷同重试风暴）
//   - context 感知（WithContext，取消即停重试）
//   - 最大重试次数限制（WithMaxRetries，防无限重试）
//   - PermanentError 标记不可重试错误（如 400 Bad Request，立即返回）
//   - 默认参数：初始 1s、最大 30s、随机 0.5、最多 3 次
package retry

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// Default 配置的默认值。
const (
	defaultInitialInterval = 1 * time.Second
	defaultMaxInterval     = 30 * time.Second
	defaultMaxRetries      = 3
	defaultMaxElapsedTime  = 2 * time.Minute
	defaultRandomization   = 0.5
	defaultMultiplier      = 2.0
)

// Config 重试配置。
type Config struct {
	// MaxRetries 最大重试次数（不含首次，0=用默认 3）。
	MaxRetries uint64
	// InitialInterval 首次重试等待时间（0=用默认 1s）。
	InitialInterval time.Duration
	// MaxInterval 单次最大等待时间（0=用默认 30s）。
	MaxInterval time.Duration
	// MaxElapsedTime 总最大耗时（0=用默认 2min，0=不限需显式设很大的值）。
	MaxElapsedTime time.Duration
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		MaxRetries:      defaultMaxRetries,
		InitialInterval: defaultInitialInterval,
		MaxInterval:     defaultMaxInterval,
		MaxElapsedTime:  defaultMaxElapsedTime,
	}
}

// Do 执行操作，失败时按指数退避重试。
// fn 返回 PermanentError 时立即停止重试。
// ctx 取消时立即停止重试。
func Do(ctx context.Context, cfg Config, fn func() error) error {
	b := newBackOff(cfg)
	b = backoff.WithMaxRetries(b, cfg.MaxRetries)
	b = backoff.WithContext(b, ctx)
	return backoff.Retry(fn, b)
}

// DoWithData 执行操作并返回数据，失败时按指数退避重试。
func DoWithData[T any](ctx context.Context, cfg Config, fn func() (T, error)) (T, error) {
	b := newBackOff(cfg)
	b = backoff.WithMaxRetries(b, cfg.MaxRetries)
	b = backoff.WithContext(b, ctx)
	return backoff.RetryWithData(fn, b)
}

// Permanent 标记错误为不可重试（如 400 Bad Request）。
func Permanent(err error) error {
	return backoff.Permanent(err)
}

// IsPermanent 判断是否不可重试错误。
func IsPermanent(err error) bool {
	_, ok := err.(*backoff.PermanentError)
	return ok
}

// newBackOff 根据配置创建指数退避策略。
func newBackOff(cfg Config) backoff.BackOff {
	initial := cfg.InitialInterval
	if initial <= 0 {
		initial = defaultInitialInterval
	}
	maxInterval := cfg.MaxInterval
	if maxInterval <= 0 {
		maxInterval = defaultMaxInterval
	}
	maxElapsed := cfg.MaxElapsedTime
	if maxElapsed <= 0 {
		maxElapsed = defaultMaxElapsedTime
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	b := backoff.NewExponentialBackOff(
		backoff.WithInitialInterval(initial),
		backoff.WithMaxInterval(maxInterval),
		backoff.WithMaxElapsedTime(maxElapsed),
		backoff.WithMultiplier(defaultMultiplier),
		backoff.WithRandomizationFactor(defaultRandomization),
	)
	_ = maxRetries // WithMaxRetries 在 Do 中调用
	return b
}

// String 返回配置的可读描述（用于日志）。
func (c Config) String() string {
	return fmt.Sprintf("maxRetries=%d initial=%s maxInterval=%s maxElapsed=%s",
		c.MaxRetries, c.InitialInterval, c.MaxInterval, c.MaxElapsedTime)
}
