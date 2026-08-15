package engine

import (
	"context"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// mockStore 测试用假知识库。
type mockStore struct {
	hits []memory.Hit
}

func (m *mockStore) Search(_ context.Context, _ memory.SearchQuery) ([]memory.Hit, error) {
	return m.hits, nil
}
func (m *mockStore) Get(_ context.Context, _ int64, _ memory.Tier) (*memory.Entry, error) {
	return nil, nil
}
func (m *mockStore) List(_ context.Context, _ memory.Tier, _, _ int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockStore) Upsert(_ context.Context, _ memory.Entry) error { return nil }
func (m *mockStore) PutCache(_ context.Context, _ memory.Entry, _ int64) error {
	return nil
}
func (m *mockStore) Delete(_ context.Context, _ int64, _ memory.Tier) error { return nil }
func (m *mockStore) Embed(_ context.Context, _ int64, _ memory.Tier, _ []float32) error {
	return nil
}
func (m *mockStore) RemoveEmbedding(_ context.Context, _ int64, _ memory.Tier) error { return nil }

var _ memory.Store = (*mockStore)(nil)

// TestMemoryGateInject 验证记忆注入。
func TestMemoryGateInject(t *testing.T) {
	store := &mockStore{hits: []memory.Hit{
		{Entry: memory.Entry{Content: "用户喜欢深色主题"}, Score: 0.9},
		{Entry: memory.Entry{Content: "用户住在北京"}, Score: 0.7},
	}}
	gate := NewMemoryGate(store)

	msgs := []llm.Message{{Role: llm.RoleUser, Content: "我喜欢什么主题？"}}
	injected, count, err := gate.Inject(context.Background(), llm.Request{Messages: msgs})
	if err != nil {
		t.Fatalf("Inject 失败: %v", err)
	}
	if count != 2 {
		t.Errorf("应注入 2 条记忆，实际 %d", count)
	}
	if len(injected) != len(msgs)+1 {
		t.Errorf("应多 1 条 system 消息，实际 %d", len(injected))
	}
	if injected[0].Role != llm.RoleSystem {
		t.Errorf("首条应是 system，实际 %s", injected[0].Role)
	}
}

// TestMemoryGateEmpty 验证无记忆时不注入。
func TestMemoryGateEmpty(t *testing.T) {
	store := &mockStore{hits: nil}
	gate := NewMemoryGate(store)

	msgs := []llm.Message{{Role: llm.RoleUser, Content: "你好"}}
	injected, count, err := gate.Inject(context.Background(), llm.Request{Messages: msgs})
	if err != nil {
		t.Fatalf("Inject 失败: %v", err)
	}
	if count != 0 {
		t.Errorf("无记忆应 count=0，实际 %d", count)
	}
	if len(injected) != len(msgs) {
		t.Errorf("无记忆不应增加消息，实际 %d", len(injected))
	}
}

// TestMemoryGateDisabled 验证禁用时不注入。
func TestMemoryGateDisabled(t *testing.T) {
	store := &mockStore{hits: []memory.Hit{{Entry: memory.Entry{Content: "x"}, Score: 0.9}}}
	gate := NewMemoryGate(store)
	gate.Enable(false)

	msgs := []llm.Message{{Role: llm.RoleUser, Content: "测试"}}
	injected, count, _ := gate.Inject(context.Background(), llm.Request{Messages: msgs})
	if count != 0 || len(injected) != len(msgs) {
		t.Errorf("禁用时应不注入，count=%d len=%d", count, len(injected))
	}
}

// TestMemoryGateShouldSkip 验证知识不足降级。
func TestMemoryGateShouldSkip(t *testing.T) {
	gate := NewMemoryGate(&mockStore{})
	gate.SetMinScore(0.7)

	hits := []memory.Hit{
		{Entry: memory.Entry{Content: "低分"}, Score: 0.3},
		{Entry: memory.Entry{Content: "也低分"}, Score: 0.2},
	}
	if !gate.ShouldSkip(hits) {
		t.Error("全低分应判定知识不足")
	}

	hits = append(hits, memory.Hit{Entry: memory.Entry{Content: "高分"}, Score: 0.9})
	if gate.ShouldSkip(hits) {
		t.Error("有高分命中不应判定知识不足")
	}
}
