// Package engine 的强制记忆门（记忆中心化，模块 26）。
// 设计：每次思考前必须走记忆检索，结果强制注入 prompt（CMA/True Memory 实证）。
// 知识不足降级：检索质量差时允许 agent 放弃知识库结果，转从其他来源获取。
package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// MemoryGate 强制记忆门。
// 在 LLM 调用前强制检索知识库，把命中结果注入 system 消息。
type MemoryGate struct {
	store    memory.Store
	enabled  bool    // 是否启用（默认开，记忆中心化红线）
	minScore float64 // 低于此分数视为"知识不足"，允许放弃
	topK     int
}

// NewMemoryGate 创建记忆门。
func NewMemoryGate(store memory.Store) *MemoryGate {
	return &MemoryGate{
		store:    store,
		enabled:  true,
		minScore: 0.0, // 默认 0（不设阈值），由检索质量自行判断
		topK:     5,
	}
}

// Inject 在 LLM 请求前检索记忆并注入。
// 返回：注入记忆后的消息列表 + 检索命中的条数。
func (g *MemoryGate) Inject(ctx context.Context, req llm.Request) ([]llm.Message, int, error) {
	if !g.enabled || g.store == nil {
		return req.Messages, 0, nil
	}

	// 用最后一条用户消息作为检索查询
	query := lastUserMessage(req.Messages)
	if query == "" {
		return req.Messages, 0, nil
	}

	hits, err := g.store.Search(ctx, memory.SearchQuery{
		Text: query,
		TopK: g.topK,
	})
	if err != nil {
		// 检索失败不阻断（降级：无记忆继续）
		return req.Messages, 0, nil
	}

	if len(hits) == 0 {
		// 无命中：提示可主动检索（RAG 3.0：Agent 自主决定多跳）
		msg := llm.Message{Role: llm.RoleSystem, Content: "记忆检索无相关内容。如需查询 minibox 项目/知识库/历史决策，请使用 search_knowledge 工具检索。"}
		messages := make([]llm.Message, 0, len(req.Messages)+1)
		messages = append(messages, msg)
		messages = append(messages, req.Messages...)
		return messages, 0, nil
	}

	// 组装记忆上下文
	var sb strings.Builder
	sb.WriteString("【记忆检索结果】以下是你记忆中与当前问题相关的内容，请直接参考使用：\n")
	for i, h := range hits {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, h.Content)
	}
	// 关键（openfang #583 实证）：记忆已注入时引导直接用，避免模型重复调用 search_knowledge 误报"没找到"
	sb.WriteString("以上记忆已自动检索并注入，请直接使用，不要对已在其中的信息再次调用 search_knowledge 工具。\n")

	// 注入为 system 消息（放在最前）
	memoryMsg := llm.Message{Role: llm.RoleSystem, Content: sb.String()}
	messages := make([]llm.Message, 0, len(req.Messages)+1)
	messages = append(messages, memoryMsg)
	messages = append(messages, req.Messages...)

	return messages, len(hits), nil
}

// ShouldSkip 判断是否应放弃记忆（知识不足降级）。
// 返回 true 表示检索质量差，agent 应转其他来源。
func (g *MemoryGate) ShouldSkip(hits []memory.Hit) bool {
	if g.minScore <= 0 {
		return false // 未设阈值，不自动跳过
	}
	for _, h := range hits {
		if h.Score >= g.minScore {
			return false // 有高分命中，不跳过
		}
	}
	return true // 全部低于阈值，知识不足
}

// Enable 开关记忆门。
func (g *MemoryGate) Enable(on bool) {
	g.enabled = on
}

// SetMinScore 设置知识不足阈值。
func (g *MemoryGate) SetMinScore(s float64) {
	g.minScore = s
}

// lastUserMessage 提取最后一条用户消息。
func lastUserMessage(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}
