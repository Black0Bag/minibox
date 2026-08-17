package llm

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

func TestFeatureRouter_Resolve(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// 创建带功能配置的 FeatureRouter
	models := &llm.FeatureModels{Configs: map[llm.Feature]llm.FeatureConfig{
		llm.FeatureAgent:      {Feature: llm.FeatureAgent, Provider: "deepseek", Model: "deepseek-chat"},
		llm.FeaturePrefExtract: {Feature: llm.FeaturePrefExtract, Provider: "glm", Model: "glm-4"},
	}}

	// 空 Router（resolve 不需要实际 provider）
	inner := &Router{entries: nil, strategy: "fallback", logger: logger}
	fr := &FeatureRouter{inner: inner, models: models, logger: logger}

	tests := []struct {
		name     string
		req      llm.Request
		want     string // 期望的 Model 值
	}{
		{
			name: "第1级: req.Model 已设置，直接返回",
			req:  llm.Request{Model: "explicit-model", Feature: llm.FeatureAgent},
			want: "explicit-model",
		},
		{
			name: "第2级: Feature 有配置，使用配置的模型",
			req:  llm.Request{Feature: llm.FeatureAgent},
			want: "deepseek-chat",
		},
		{
			name: "第2级: 另一个 Feature 配置",
			req:  llm.Request{Feature: llm.FeaturePrefExtract},
			want: "glm-4",
		},
		{
			name: "第3级: Feature 未配置，返回空",
			req:  llm.Request{Feature: llm.FeatureSubagent},
			want: "",
		},
		{
			name: "第3级: 无 Feature，返回空",
			req:  llm.Request{},
			want: "",
		},
		{
			name: "第1级优先: Model 和 Feature 同时设置",
			req:  llm.Request{Model: "override", Feature: llm.FeatureAgent},
			want: "override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fr.resolve(tt.req)
			if err != nil {
				t.Fatalf("resolve() error = %v", err)
			}
			if got.Model != tt.want {
				t.Errorf("resolve() Model = %q, want %q", got.Model, tt.want)
			}
		})
	}
}

func TestFeatureRouter_UpdateModels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	inner := &Router{entries: nil, strategy: "fallback", logger: logger}
	fr := NewFeatureRouter(inner, nil, logger)

	// 初始应为空
	if fr.FeatureModels() == nil || fr.FeatureModels().Configs == nil {
		t.Error("初始 FeatureModels 不应为 nil")
	}

	// 更新配置
	models := &llm.FeatureModels{Configs: map[llm.Feature]llm.FeatureConfig{
		llm.FeatureAgent: {Feature: llm.FeatureAgent, Model: "test-model"},
	}}
	fr.UpdateModels(models)

	got := fr.FeatureModels().Get(llm.FeatureAgent)
	if got.Model != "test-model" {
		t.Errorf("更新后 Model = %q, want %q", got.Model, "test-model")
	}
}

func TestFeatureRouter_Complete_Delegation(t *testing.T) {
	// 验证 Complete 委托给 inner Router
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// 创建带 mock provider 的 Router
	provider := &mockProvider{name: "mock"}
	entry := NewProviderEntry(provider, "mock")
	inner := NewRouter([]*ProviderEntry{entry}, RouterConfig{Strategy: "fallback"}, logger)
	fr := NewFeatureRouter(inner, nil, logger)

	resp, err := fr.Complete(context.Background(), llm.Request{Model: "test"})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp == nil || resp.Content != "mock-response" {
		t.Errorf("Complete() Content = %v, want %q", resp, "mock-response")
	}
}

// mockProvider 实现 llm.Provider 接口用于测试
type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{
		Content: "mock-response",
		Model:   req.Model,
	}, nil
}

func (m *mockProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, nil
}

func (m *mockProvider) Models(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}
