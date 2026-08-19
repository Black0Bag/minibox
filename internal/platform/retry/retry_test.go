package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoSuccessOnFirstTry(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), DefaultConfig(), func() error {
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Do 返回错误: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("调用次数 = %d, want 1", calls.Load())
	}
}

func TestDoRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Config{
		MaxRetries:      3,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		MaxElapsedTime:  100 * time.Millisecond,
	}, func() error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("临时失败")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do 返回错误: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("调用次数 = %d, want 3", calls.Load())
	}
}

func TestDoRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Config{
		MaxRetries:      2,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		MaxElapsedTime:  100 * time.Millisecond,
	}, func() error {
		calls.Add(1)
		return errors.New("持续失败")
	})
	if err == nil {
		t.Fatal("Do 应返回错误")
	}
	// MaxRetries=2 意味着重试 2 次 + 首次 = 3 次
	if calls.Load() != 3 {
		t.Errorf("调用次数 = %d, want 3", calls.Load())
	}
}

func TestDoPermanentError(t *testing.T) {
	var calls atomic.Int32
	err := Do(context.Background(), Config{
		MaxRetries:      5,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		MaxElapsedTime:  100 * time.Millisecond,
	}, func() error {
		calls.Add(1)
		return Permanent(errors.New("不可重试错误"))
	})
	if err == nil {
		t.Fatal("Do 应返回错误")
	}
	if calls.Load() != 1 {
		t.Errorf("PermanentError 应立即返回，调用次数 = %d, want 1", calls.Load())
	}
}

func TestDoContextCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	var calls atomic.Int32
	_ = Do(ctx, Config{
		MaxRetries:      10,
		InitialInterval: 50 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
		MaxElapsedTime:  10 * time.Second,
	}, func() error {
		calls.Add(1)
		return errors.New("持续失败")
	})
	// 应在 context 超时后停止，不会跑满 10 次
	if calls.Load() > 3 {
		t.Errorf("context 取消后应停止重试，调用次数 = %d", calls.Load())
	}
}

func TestDoWithData(t *testing.T) {
	var calls atomic.Int32
	result, err := DoWithData(context.Background(), Config{
		MaxRetries:      3,
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		MaxElapsedTime:  100 * time.Millisecond,
	}, func() (string, error) {
		n := calls.Add(1)
		if n < 2 {
			return "", errors.New("临时失败")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("DoWithData 返回错误: %v", err)
	}
	if result != "ok" {
		t.Errorf("结果 = %q, want %q", result, "ok")
	}
}

func TestIsPermanent(t *testing.T) {
	pe := Permanent(errors.New("test"))
	if !IsPermanent(pe) {
		t.Error("IsPermanent 应返回 true")
	}
	if IsPermanent(errors.New("test")) {
		t.Error("IsPermanent 对普通错误应返回 false")
	}
}

func TestConfigString(t *testing.T) {
	c := Config{MaxRetries: 3, InitialInterval: 1 * time.Second, MaxInterval: 30 * time.Second, MaxElapsedTime: 2 * time.Minute}
	s := c.String()
	if s == "" {
		t.Error("Config.String() 不应为空")
	}
}
