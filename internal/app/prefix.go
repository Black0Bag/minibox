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

// prefExtractor 用 LLM 从候选内容提取结构化偏好（B13 LLM 机制）。
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
		Feature:  llm.FeaturePrefExtract, // B6 偏好蒸馏使用独立模型
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
