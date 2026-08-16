package tools

// 知识库检索工具（RAG 3.0：Agent 自主决定何时/如何检索）。
// 设计（联网校准 kapa.ai + multigrid + Agently 2026）：
//   - 工具描述按语料定制："搜索 minibox 内部知识库..."（multigrid：按语料描述而非"搜索知识库"）
//   - 描述明确"结果可能不相关"，让模型保持怀疑（kapa.ai）
//   - 结构化输出（编号+内容），供引用；顶层提示不确定就明说
//   - 三级降级：embedder 可用走 Hybrid(vec+FTS5)，否则 FTS5→LIKE（复用 memory.Store.Search）

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// SearchKnower 检索执行能力（组合根注入：memory.Store + 可选 query 向量）。
type SearchKnower interface {
	// Search 混合检索（三级降级）。
	Search(ctx context.Context, q memory.SearchQuery) ([]memory.Hit, error)
}

// QueryEmbedder 查询向量化（可选，asymmetric 模型 query 模式）。
type QueryEmbedder interface {
	// EmbedQuery 生成查询向量。
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// kbSearchTool search_knowledge 工具实现。
type kbSearchTool struct {
	store    SearchKnower
	embedder QueryEmbedder // 可选
}

// NewSearchKnowledge 创建 search_knowledge 工具。
// embedder 可空：为空时走纯文本检索（FTS5→LIKE 降级）。
func NewSearchKnowledge(store SearchKnower, embedder QueryEmbedder) tools.Tool {
	return &kbSearchTool{store: store, embedder: embedder}
}

// Name 工具名。
func (t *kbSearchTool) Name() string { return "search_knowledge" }

// Description 按语料定制（multigrid：描述按语料而非泛泛能力）。
func (t *kbSearchTool) Description() string {
	return "在 minibox 内部知识库中语义检索，覆盖项目架构决策、历史进度记录、用户偏好、编码规范、运维知识。" +
		"当你需要回答与 minibox 项目/用户偏好/历史决策相关的问题时使用。返回的相关片段按相关性排序；" +
		"注意：返回内容可能与查询弱相关甚至不相关，请先判断相关性再使用，信息不足时明确说明。"
}

// JSONSchema 输入参数 schema（OpenAI function calling 标准）。
func (t *kbSearchTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "检索查询，用自然语言描述想找的知识，例如'项目的数据库设计决策'"
			},
			"top_k": {
				"type": "integer",
				"description": "返回的片段数上限（1-10，默认 5）",
				"minimum": 1,
				"maximum": 10
			}
		},
		"required": ["query"],
		"additionalProperties": false
	}`)
}

// Metadata 权限元数据（只读、搜索类、低风险）。
func (t *kbSearchTool) Metadata() tools.Metadata {
	return tools.Metadata{
		ReadOnly:        true,
		SearchOrRead:    true,
		ConcurrencySafe: true,
		MaxResultSize:   8000,
		RiskTier:        "low",
	}
}

// Invoke 执行检索。
func (t *kbSearchTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		TopK  *int   `json:"top_k"`
	}
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("缺少参数 query")
	}
	topK := 5
	if args.TopK != nil {
		topK = *args.TopK
		if topK < 1 {
			topK = 1
		}
		if topK > 10 {
			topK = 10
		}
	}

	// 构建检索请求（embedder 可用时生成 query 向量走 hybrid）
	q := memory.SearchQuery{Text: args.Query, TopK: topK, Tier: memory.TierStore}
	if t.embedder != nil {
		if vec, err := t.embedder.EmbedQuery(ctx, args.Query); err == nil && len(vec) > 0 {
			q.QueryVector = vec
		}
	}

	hits, err := t.store.Search(ctx, q)
	if err != nil {
		return "", fmt.Errorf("知识库检索失败: %w", err)
	}
	if len(hits) == 0 {
		return "知识库中未检索到相关内容。", nil
	}

	// 结构化输出（编号 + 内容，供 Agent 引用判断）
	var sb strings.Builder
	sb.WriteString("知识库检索结果（按相关性排序）：\n")
	for i, h := range hits {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, h.Content))
		if h.Source != "" {
			sb.WriteString(fmt.Sprintf("    (来源: %s)\n", h.Source))
		}
	}
	sb.WriteString("以上结果可能不相关，请判断后使用；信息不足请明确说明。")
	return sb.String(), nil
}

var _ tools.Tool = (*kbSearchTool)(nil)
