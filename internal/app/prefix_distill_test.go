package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

func TestKnowledgeExtractorStructuredJSONAndValidation(t *testing.T) {
	provider := &prefExtractorTestProvider{
		content: `[{"content":"minibox 使用 SQLite 保存知识。","tags":["架构","存储"],"importance":1.4}]`,
	}
	entries, err := newKnowledgeExtractor(provider).Extract(context.Background(), "原始资料")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "minibox 使用 SQLite 保存知识。" || len(entries[0].Tags) != 2 || entries[0].Importance != 1 {
		t.Fatalf("entries=%+v", entries)
	}
	if provider.lastRequest.Feature != llm.FeatureKnowledgeCompile || len(provider.lastRequest.Messages) != 2 {
		t.Fatalf("knowledge compile request=%+v", provider.lastRequest)
	}
}

func TestKnowledgeExtractorRejectsInvalidOrEmptyOutput(t *testing.T) {
	for _, content := range []string{"not-json", "[]", `[{"content":"  "}]`} {
		_, err := newKnowledgeExtractor(&prefExtractorTestProvider{content: content}).Extract(context.Background(), "原始资料")
		if err == nil {
			t.Fatalf("content=%q 应返回错误", content)
		}
	}
}

func TestPrefExtractorStructuredJSONAndMarkdownFence(t *testing.T) {
	provider := &prefExtractorTestProvider{
		content: "```json\n[{\"key\":\"主题\",\"value\":\"偏好深色主题\",\"probability\":0.92}]\n```",
	}
	prefs, err := newPrefExtractor(provider).Extract(context.Background(), "用户喜欢深色主题")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(prefs) != 1 || prefs[0].Key != "主题" || prefs[0].Value != "偏好深色主题" || prefs[0].Probability != 0.92 {
		t.Fatalf("structured prefs=%+v", prefs)
	}
	if provider.lastRequest.Feature != llm.FeaturePrefExtract || len(provider.lastRequest.Messages) != 2 {
		t.Fatalf("pref extract request=%+v", provider.lastRequest)
	}
}

func TestPrefExtractorRejectsInvalidJSON(t *testing.T) {
	_, err := newPrefExtractor(&prefExtractorTestProvider{content: "not-json"}).Extract(context.Background(), "内容")
	if err == nil || !strings.Contains(err.Error(), "解析偏好提取结果失败") {
		t.Fatalf("invalid JSON error=%v", err)
	}
}

type prefExtractorTestProvider struct {
	content     string
	lastRequest llm.Request
}

func (p *prefExtractorTestProvider) Name() string { return "pref-fixture" }

func (p *prefExtractorTestProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.lastRequest = req
	return &llm.Response{Content: p.content}, nil
}

func (p *prefExtractorTestProvider) Stream(context.Context, llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("stream not used")
}

func (p *prefExtractorTestProvider) Models(context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}
