package llm

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	domainllm "github.com/Black0Bag/minibox/internal/domain/llm"
)

// fakeProvider 测试用假供应商。
type fakeProvider struct {
	name      string
	failCount int
	callCount int
	err       error
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Complete(_ context.Context, _ domainllm.Request) (*domainllm.Response, error) {
	f.callCount++
	if f.failCount > 0 {
		f.failCount--
		return nil, f.err
	}
	return &domainllm.Response{Content: "from-" + f.name}, nil
}
func (f *fakeProvider) Stream(_ context.Context, _ domainllm.Request) (<-chan domainllm.StreamEvent, error) {
	return nil, errors.New("stream not mocked")
}
func (f *fakeProvider) Models(_ context.Context) ([]domainllm.ModelInfo, error) {
	return nil, nil
}

var _ domainllm.Provider = (*fakeProvider)(nil)

// TestRouterFallback 验证级联 fallback。
func TestRouterFallback(t *testing.T) {
	logger := slog.Default()
	p1 := &fakeProvider{name: "p1", failCount: 1, err: errors.New("p1 down")}
	p2 := &fakeProvider{name: "p2"}

	e1 := NewProviderEntry(p1, "p1")
	e2 := NewProviderEntry(p2, "p2")

	r := NewRouter([]*ProviderEntry{e1, e2}, RouterConfig{Strategy: "fallback"}, logger)
	resp, err := r.Complete(context.Background(), domainllm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("fallback 失败: %v", err)
	}
	if resp.Content != "from-p2" {
		t.Errorf("期望 p2 响应，实际 %s", resp.Content)
	}
	if p1.callCount != 1 || p2.callCount != 1 {
		t.Errorf("调用次数错误: p1=%d p2=%d", p1.callCount, p2.callCount)
	}
}

// TestRouterAllFail 验证全部失败返回错误。
func TestRouterAllFail(t *testing.T) {
	logger := slog.Default()
	p1 := &fakeProvider{name: "p1", failCount: 100, err: errors.New("down")}
	p2 := &fakeProvider{name: "p2", failCount: 100, err: errors.New("down")}

	e1 := NewProviderEntry(p1, "p1")
	e2 := NewProviderEntry(p2, "p2")

	r := NewRouter([]*ProviderEntry{e1, e2}, RouterConfig{Strategy: "fallback"}, logger)
	_, err := r.Complete(context.Background(), domainllm.Request{Model: "m"})
	if err == nil {
		t.Fatal("全部失败应返回错误")
	}
}

// TestRouterRoundRobin 验证轮询。
func TestRouterRoundRobin(t *testing.T) {
	logger := slog.Default()
	p1 := &fakeProvider{name: "p1"}
	p2 := &fakeProvider{name: "p2"}

	e1 := NewProviderEntry(p1, "p1")
	e2 := NewProviderEntry(p2, "p2")

	r := NewRouter([]*ProviderEntry{e1, e2}, RouterConfig{Strategy: "round-robin"}, logger)

	// 两次调用应轮询到两个供应商
	resp1, _ := r.Complete(context.Background(), domainllm.Request{Model: "m"})
	resp2, _ := r.Complete(context.Background(), domainllm.Request{Model: "m"})

	if resp1.Content == resp2.Content {
		t.Errorf("轮询应分配不同供应商，两次都是 %s", resp1.Content)
	}
}
