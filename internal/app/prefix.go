package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/infrastructure/storage"
)

// knowledgeExtractor 用 FeatureKnowledgeCompile 模型提炼知识条目。
type knowledgeExtractor struct {
	llm    llm.Provider
	prompt string
}

func newKnowledgeExtractor(p llm.Provider) *knowledgeExtractor {
	return &knowledgeExtractor{
		llm: p,
		prompt: "你是知识编译器。把输入提炼成可检索的知识条目。只输出 JSON 数组，" +
			"每项必须是 {content,tags,importance}，content 为完整事实，tags 为字符串数组，" +
			"importance 为 0 到 1 的数字。不要输出 Markdown、解释或代码围栏。",
	}
}

func (e *knowledgeExtractor) Extract(ctx context.Context, source string) ([]memory.Entry, error) {
	resp, err := e.llm.Complete(ctx, llm.Request{
		Feature: llm.FeatureKnowledgeCompile,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: e.prompt},
			{Role: llm.RoleUser, Content: source},
		},
		MaxTokens: intPtr(1200),
	})
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var items []struct {
		Content    string   `json:"content"`
		Tags       []string `json:"tags"`
		Importance float64  `json:"importance"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("解析知识编译结果失败: %w", err)
	}
	out := make([]memory.Entry, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		if item.Importance < 0 {
			item.Importance = 0
		}
		if item.Importance > 1 {
			item.Importance = 1
		}
		out = append(out, memory.Entry{Content: item.Content, Tags: item.Tags, Importance: item.Importance})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("知识编译结果没有有效条目")
	}
	return out, nil
}

// 设计：一次 LLM 调用，prompt 要求输出 JSON [{key, value, probability}]。
type prefExtractor struct {
	llm    llm.Provider
	prompt string
}

// newPrefExtractor 创建 LLM 偏好提取器。
func newPrefExtractor(p llm.Provider) *prefExtractor {
	return &prefExtractor{
		llm: p,
		prompt: "你是一个偏好蒸馏器。从下面的内容中提取用户的偏好或关键行为准则。" +
			"输出 JSON 数组，每项 {key, value, probability}，" +
			"key 是偏好主题（简短），value 是偏好内容，" +
			"probability 是 0-1 的置信度。只输出 JSON，不要解释。",
	}
}

// Extract 实现 storage.PrefExtractor。
func (e *prefExtractor) Extract(ctx context.Context, content string) ([]memory.Preference, error) {
	resp, err := e.llm.Complete(ctx, llm.Request{
		Feature: llm.FeaturePrefExtract, // B6 偏好蒸馏使用独立模型
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: e.prompt},
			{Role: llm.RoleUser, Content: content},
		},
		MaxTokens: intPtr(500),
	})
	if err != nil {
		return nil, err
	}

	// 解析 JSON 数组（容忍 markdown 代码块包裹）
	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var items []struct {
		Key         string  `json:"key"`
		Value       string  `json:"value"`
		Probability float64 `json:"probability"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("解析偏好提取结果失败: %w", err)
	}

	var out []memory.Preference
	for _, it := range items {
		if it.Key == "" {
			continue
		}
		out = append(out, memory.Preference{
			Key:         it.Key,
			Value:       it.Value,
			Probability: it.Probability,
		})
	}
	return out, nil
}

var _ storage.PrefExtractor = (*prefExtractor)(nil)

// intPtr 返回 int 指针（LLM MaxTokens 可空参数用）。
func intPtr(v int) *int { return &v }
