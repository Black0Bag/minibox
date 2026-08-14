package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// SQLiteAssembler 上下文组装器（投影层）。
// 依据：Zylos 2026 "上下文窗口是投影不是存储"；tianpan 2026 "稳定前缀+预算封顶+思考保留区"。
type SQLiteAssembler struct {
	store memory.Store
	// pinnedProvider 稳定前缀提供者（system prompt/角色卡/世界书，由外部注入）。
	pinnedProvider func() []string
}

// NewAssembler 创建上下文组装器。
func NewAssembler(store memory.Store) *SQLiteAssembler {
	return &SQLiteAssembler{store: store}
}

// SetPinnedProvider 设置稳定前缀提供者（Phase 4 接入角色卡/世界书）。
func (a *SQLiteAssembler) SetPinnedProvider(fn func() []string) {
	a.pinnedProvider = fn
}

// Assemble 组装上下文投影。
// 预算分配（tianpan 2026 实证）：
//   - Pinned（稳定前缀）：10-15%
//   - Retrieved（检索）：30-40%
//   - Recent（最近对话）：剩余
//   - 保留：思考区（不占 retrieved/recent）
func (a *SQLiteAssembler) Assemble(ctx context.Context, current string, budget int) (*memory.Projection, error) {
	if budget == 0 {
		budget = 8000 // 默认预算
	}

	proj := &memory.Projection{
		Usage: memory.TokenUsage{Budget: budget},
	}

	// 1. 稳定前缀（缓存命中区）
	if a.pinnedProvider != nil {
		proj.Pinned = a.pinnedProvider()
	}
	// 简化 token 估算：汉字 1 token/字 近似
	for _, p := range proj.Pinned {
		proj.Usage.Pinned += estimateTokens(p)
	}

	// 2. 动态检索（放稳定内容后面，缓存拓扑正确性）
	retrievedBudget := int(float64(budget) * 0.4)
	hits, err := a.store.Search(ctx, memory.SearchQuery{
		Text: current,
		TopK: 5,
	})
	if err != nil {
		return nil, fmt.Errorf("上下文组装检索失败: %w", err)
	}
	for _, h := range hits {
		if proj.Usage.Retrieved+estimateTokens(h.Content) > retrievedBudget {
			break
		}
		proj.Retrieved = append(proj.Retrieved, h)
		proj.Usage.Retrieved += estimateTokens(h.Content)
	}

	// 3. 剩余预算给 recent
	proj.Usage.Recent = budget - proj.Usage.Pinned - proj.Usage.Retrieved
	if proj.Usage.Recent < 0 {
		proj.Usage.Recent = 0
	}
	proj.Usage.Total = proj.Usage.Pinned + proj.Usage.Retrieved

	return proj, nil
}

// estimateTokens 简化 token 估算（汉字 1 token/字，英文按空格）。
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	// 粗略：中文字符数 + 英文词数
	cjk := 0
	words := strings.FieldsFunc(s, func(r rune) bool {
		if r >= 0x4e00 && r <= 0x9fff {
			cjk++
			return true
		}
		return r == ' ' || r == '\n' || r == '\t' || r == '，' || r == '。'
	})
return cjk + len(words)
}
