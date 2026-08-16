package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// fakeKB 测试用假知识库。
type fakeKB struct {
	hits []memory.Hit
	err  error
}

func (f *fakeKB) Search(_ context.Context, _ memory.SearchQuery) ([]memory.Hit, error) {
	return f.hits, f.err
}

var _ SearchKnower = (*fakeKB)(nil)

// fakeEmbedder 测试用假查询向量化器。
type fakeEmbedder struct{ called bool }

func (f *fakeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	f.called = true
	return make([]float32, 8), nil
}

var _ QueryEmbedder = (*fakeEmbedder)(nil)

func TestSearchKnowledge_Invoke(t *testing.T) {
	kb := &fakeKB{hits: []memory.Hit{
		{Entry: memory.Entry{Content: "知识库是唯一记忆系统", Source: "决策记录"}, Score: 0.9},
		{Entry: memory.Entry{Content: "项目采用 Go 后端"}, Score: 0.7},
	}}
	em := &fakeEmbedder{}
	tool := NewSearchKnowledge(kb, em)

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"记忆系统","top_k":2}`))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	if !strings.Contains(out, "知识库是唯一记忆系统") {
		t.Errorf("输出应包含命中内容: %s", out)
	}
	if !strings.Contains(out, "来源: 决策记录") {
		t.Errorf("输出应包含来源: %s", out)
	}
	if !em.called {
		t.Error("embedder 应被调用（query 向量）")
	}
}

func TestSearchKnowledge_NoMatch(t *testing.T) {
	tool := NewSearchKnowledge(&fakeKB{hits: nil}, nil)
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"不存在的内容"}`))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	if !strings.Contains(out, "未检索到") {
		t.Errorf("无命中应返回提示: %s", out)
	}
}

func TestSearchKnowledge_MissingQuery(t *testing.T) {
	tool := NewSearchKnowledge(&fakeKB{}, nil)
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Error("缺 query 应报错")
	}
}

func TestSearchKnowledge_TopKClamp(t *testing.T) {
	kb := &fakeKB{hits: []memory.Hit{
		{Entry: memory.Entry{Content: "a"}},
		{Entry: memory.Entry{Content: "b"}},
	}}
	tool := NewSearchKnowledge(kb, nil)
	// top_k=0 → 钳到 1；top_k=99 → 钳到 10
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"q","top_k":0}`)); err != nil {
		t.Fatalf("top_k=0 应钳制: %v", err)
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"q","top_k":99}`)); err != nil {
		t.Fatalf("top_k=99 应钳制: %v", err)
	}
}

func TestSearchKnowledge_Metadata(t *testing.T) {
	tool := NewSearchKnowledge(&fakeKB{}, nil)
	if tool.Name() != "search_knowledge" {
		t.Errorf("工具名 = %q", tool.Name())
	}
	meta := tool.Metadata()
	if !meta.ReadOnly || !meta.SearchOrRead || meta.RiskTier != "low" {
		t.Errorf("元数据错误: %+v", meta)
	}
	if !strings.Contains(tool.Description(), "minibox 内部知识库") {
		t.Errorf("描述应按语料定制: %s", tool.Description())
	}
	var _ tools.Tool = tool
}
