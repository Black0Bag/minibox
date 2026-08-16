// Package memory 定义知识库/记忆的统一接口。
// 设计：知识库=唯一记忆系统（PRD B11 红线）。
// 三级降级检索（Hybrid→FTS5→LIKE，mika ADR-003 实证）。
// 记忆中心化：检索即写入（CMA），强制记忆门（Phase 4 接 Agent 引擎）。
package memory

import "context"

// Tier 知识库层级（双区制，B11）。
type Tier string

const (
	// TierStore 存储区（正式知识，编译沉淀）。
	TierStore Tier = "store"
	// TierCache 缓存区（临时片段，带 TTL）。
	TierCache Tier = "cache"
)

// Entry 知识库条目（kb_store/kb_cache 统一视图）。
type Entry struct {
	ID          int64    `json:"id"`
	Content     string   `json:"content"`
	Source      string   `json:"source,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	SourceHash  string   `json:"source_hash,omitempty"`
	Importance  float64  `json:"importance"`
	AccessCount int      `json:"access_count"`
	Tier        Tier     `json:"tier"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	ExpiresAt   *string  `json:"expires_at,omitempty"` // 仅缓存区
}

// Hit 检索命中项。
type Hit struct {
	Entry
	Score     float64 `json:"score"`
	MatchType string  `json:"match_type"` // fts / vec / both
}

// SearchQuery 检索查询。
type SearchQuery struct {
	Text      string
	Tier      Tier
	TopK      int
	MinScore  float64 // 相似度阈值（默认 0.7，Bedrock 实证）
	MaxTokens int     // 预算限制
	// QueryVector 查询向量（Hybrid 检索用）。非空时走向量 KNN + FTS5 融合；
	// 为空时降级 FTS5→LIKE（三级降级落地，vstash/mika 实证）。
	QueryVector []float32
}

// Store 知识库存储接口。
type Store interface {
	// Search 混合检索（FTS5 + vec + RRF 融合）。
	Search(ctx context.Context, q SearchQuery) ([]Hit, error)

	// Get 获取单条。
	Get(ctx context.Context, id int64, tier Tier) (*Entry, error)

	// List 分页列出。
	List(ctx context.Context, tier Tier, offset, limit int) ([]Entry, error)

	// Upsert 写入（存储区）。
	Upsert(ctx context.Context, e Entry) error

	// PutCache 写入缓存区（带 TTL）。
	PutCache(ctx context.Context, e Entry, ttlSeconds int64) error

	// Delete 删除。
	Delete(ctx context.Context, id int64, tier Tier) error

	// Embed 写入/更新某条目的向量（编译管道 embedding 后调用，模块 18/19）。
	// dim 必须与 schema_meta.embedding_dim 一致，否则拒绝。
	Embed(ctx context.Context, id int64, tier Tier, vec []float32) error

	// GetIDByHash 通过 source_hash 查条目 ID（编译管道幂等摄入用）。
	GetIDByHash(ctx context.Context, sourceHash string) (int64, error)

	// RemoveEmbedding 删除某条目的向量（内容回滚/重建时）。
	RemoveEmbedding(ctx context.Context, id int64, tier Tier) error
}
