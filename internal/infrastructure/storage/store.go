package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// DefaultVecDim 默认向量维度（与 0001/0002 迁移 kb_vec embedding float[1024] 一致）。
const DefaultVecDim = 1024

// SQLiteStore 基于 SQLite 的知识库存储实现。
// 表：kb_store（存储区）/ kb_cache（缓存区）/ kb_fts（FTS5）/ kb_vec（sqlite-vec）。
type SQLiteStore struct {
	db        *sql.DB
	tokenizer memory.Tokenizer
	vecDim    int // 向量维度（来自配置；0 或 <0 时回落 DefaultVecDim）
}

// NewSQLiteStore 创建 SQLite 知识库存储。
// vecDim 来自 cfg.Embedding.Dimensions（0=模型默认，回落 DefaultVecDim 1024）。
// vec0 虚表维度在创建时固定，切换 embedding 模型须重建 kb_vec 索引（schema_meta 监测）。
func NewSQLiteStore(db *sql.DB, tokenizer memory.Tokenizer, vecDim int) *SQLiteStore {
	if vecDim <= 0 {
		vecDim = DefaultVecDim
	}
	return &SQLiteStore{db: db, tokenizer: tokenizer, vecDim: vecDim}
}

// VecDim 返回当前向量维度（配置接入后，与 kb_vec 索引一致）。
func (s *SQLiteStore) VecDim() int { return s.vecDim }

// Search 混合检索（三级降级：Hybrid→FTS5→LIKE，mika ADR-003 实证）。
// 有 QueryVector → Hybrid（vec KNN + FTS5 + RRF 融合）；
// 无向量 → FTS5 → LIKE 降级。
// 任何一级空结果/失败都会继续降级（保证不因单级空结果丢失检索能力）。
func (s *SQLiteStore) Search(ctx context.Context, q memory.SearchQuery) ([]memory.Hit, error) {
	if strings.TrimSpace(q.Text) == "" {
		return nil, nil
	}
	if q.TopK == 0 {
		q.TopK = 10
	}
	if q.Tier == "" {
		q.Tier = memory.TierStore
	}
	if q.MinScore == 0 {
		q.MinScore = 0.0
	}
	table := tierTable(q.Tier)

	// 1. 有查询向量 → Hybrid（向量 + FTS5 融合）
	if len(q.QueryVector) > 0 {
		hits, err := s.hybridSearch(ctx, q.Tier, q)
		if err == nil && len(hits) > 0 {
			return hits, nil
		}
		// hybrid 失败或空结果 → 继续降级（不因向量空结果丢失检索）
		ftsHits, ftsErr := s.searchFTS(ctx, table, q)
		if ftsErr == nil && len(ftsHits) > 0 {
			return ftsHits, nil
		}
		likeHits, likeErr := s.searchLike(ctx, table, q)
		if likeErr == nil && len(likeHits) > 0 {
			return likeHits, nil
		}
		// 全部降级都空：返回最上层错误（若有）
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	// 2. 无向量 → FTS5
	hits, err := s.searchFTS(ctx, table, q)
	if err != nil || len(hits) > 0 {
		return hits, err
	}

	// 3. 降级到 LIKE（FTS5 无结果或不可用）
	return s.searchLike(ctx, table, q)
}

// searchFTS 通过 FTS5 全文索引检索。
func (s *SQLiteStore) searchFTS(ctx context.Context, table string, q memory.SearchQuery) ([]memory.Hit, error) {
	if s.tokenizer == nil {
		return nil, nil
	}
	matchQuery, err := s.tokenizer.TokenizeQuery(q.Text)
	if err != nil || matchQuery == "" {
		return nil, err
	}

	// #nosec G201 -- table 来自 tierTable() 白名单（仅 kb_store/kb_cache），非用户输入
	query := fmt.Sprintf(`
		SELECT %s.id, %s.content, %s.source, %s.tags, %s.source_hash,
		       %s.importance, %s.access_count, %s.created_at,
		       bm25(kb_fts) as score
		FROM kb_fts
		JOIN %s ON %s.id = kb_fts.rowid
		WHERE kb_fts MATCH ?
		ORDER BY score
		LIMIT ?`, table, table, table, table, table, table, table, table, table, table)

	rows, err := s.db.QueryContext(ctx, query, matchQuery, q.TopK)
	if err != nil {
		return nil, fmt.Errorf("FTS5 检索失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []memory.Hit
	for rows.Next() {
		var h memory.Hit
		var tags string
		if err := rows.Scan(&h.ID, &h.Content, &h.Source, &tags, &h.SourceHash,
			&h.Importance, &h.AccessCount, &h.CreatedAt, &h.Score); err != nil {
			return nil, err
		}
		h.Tags = parseTags(tags)
		h.MatchType = "fts"
		h.Tier = q.Tier
		// FTS5 bm25 越小（通常是负值）代表越相关；MinScore 属于向量相似度语义，
		// 不应用于 FTS bm25，否则会过滤掉所有正常 FTS 命中。
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// searchLike 降级：LIKE 子串匹配（索引未就绪/embedding 不可用）。
func (s *SQLiteStore) searchLike(ctx context.Context, table string, q memory.SearchQuery) ([]memory.Hit, error) {
	// #nosec G201 -- table 来自 tierTable() 白名单（仅 kb_store/kb_cache），非用户输入
	query := fmt.Sprintf(`
		SELECT id, content, source, tags, source_hash, importance, access_count, created_at
		FROM %s
		WHERE content LIKE ?
		ORDER BY importance DESC
		LIMIT ?`, table)

	like := "%" + q.Text + "%"
	rows, err := s.db.QueryContext(ctx, query, like, q.TopK)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var hits []memory.Hit
	for rows.Next() {
		var h memory.Hit
		var tags string
		if err := rows.Scan(&h.ID, &h.Content, &h.Source, &tags, &h.SourceHash,
			&h.Importance, &h.AccessCount, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Tags = parseTags(tags)
		h.MatchType = "like"
		h.Tier = q.Tier
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// Get 获取单条。
func (s *SQLiteStore) Get(ctx context.Context, id int64, tier memory.Tier) (*memory.Entry, error) {
	table := tierTable(tier)
	// #nosec G201 -- table 来自 tierTable() 白名单（仅 kb_store/kb_cache），非用户输入
	query := fmt.Sprintf(`
		SELECT id, content, source, tags, source_hash, importance, access_count, created_at, updated_at
		FROM %s WHERE id = ?`, table)

	var e memory.Entry
	var tags string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&e.ID, &e.Content, &e.Source, &tags, &e.SourceHash,
		&e.Importance, &e.AccessCount, &e.CreatedAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("条目不存在: %d", id)
	}
	if err != nil {
		return nil, err
	}
	e.Tags = parseTags(tags)
	e.Tier = tier
	e.UpdatedAt = updatedAt
	return &e, nil
}

// List 分页列出。
func (s *SQLiteStore) List(ctx context.Context, tier memory.Tier, offset, limit int) ([]memory.Entry, error) {
	table := tierTable(tier)
	// #nosec G201 -- table 来自 tierTable() 白名单（仅 kb_store/kb_cache），非用户输入
	query := fmt.Sprintf(`
		SELECT id, content, source, tags, source_hash, importance, access_count, created_at, updated_at
		FROM %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, table)

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []memory.Entry
	for rows.Next() {
		var e memory.Entry
		var tags, updatedAt string
		if err := rows.Scan(&e.ID, &e.Content, &e.Source, &tags, &e.SourceHash,
			&e.Importance, &e.AccessCount, &e.CreatedAt, &updatedAt); err != nil {
			return nil, err
		}
		e.Tags = parseTags(tags)
		e.Tier = tier
		e.UpdatedAt = updatedAt
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Upsert 写入存储区。
// 幂等：source_hash 唯一约束，重复写入是 no-op（SmartSearch 实证）。
func (s *SQLiteStore) Upsert(ctx context.Context, e memory.Entry) error {
	if s.tokenizer == nil {
		return fmt.Errorf("知识库分词器未就绪")
	}
	tokenized, err := s.tokenizer.TokenizeIndex(e.Content)
	if err != nil {
		return fmt.Errorf("分词失败: %w", err)
	}
	tagsJSON, err := json.Marshal(e.Tags)
	if err != nil {
		return fmt.Errorf("序列化 tags 失败: %w", err)
	}

	if e.ID > 0 {
		// 若客户端未提供 source_hash（HTTP PATCH 场景），保留数据库原值避免 UNIQUE 约束冲突。
		var result sql.Result
		if e.SourceHash != "" {
			result, err = s.db.ExecContext(ctx, `
				UPDATE kb_store
				SET content = ?, tokenized_content = ?, source = ?, tags = ?,
				    source_hash = ?, importance = ?, updated_at = datetime('now')
				WHERE id = ?`,
				e.Content, tokenized, e.Source, string(tagsJSON), e.SourceHash, e.Importance, e.ID)
		} else {
			result, err = s.db.ExecContext(ctx, `
				UPDATE kb_store
				SET content = ?, tokenized_content = ?, source = ?, tags = ?,
				    importance = ?, updated_at = datetime('now')
				WHERE id = ?`,
				e.Content, tokenized, e.Source, string(tagsJSON), e.Importance, e.ID)
		}
		if err != nil {
			return fmt.Errorf("更新知识条目失败: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("读取知识条目更新结果失败: %w", err)
		}
		if changed == 0 {
			return fmt.Errorf("条目不存在: %d", e.ID)
		}
		return nil
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kb_store (content, tokenized_content, source, tags, source_hash, importance)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_hash) DO NOTHING`,
		e.Content, tokenized, e.Source, string(tagsJSON), e.SourceHash, e.Importance)
	if err != nil {
		return fmt.Errorf("写入知识条目失败: %w", err)
	}
	return nil
}

// PutCache 写入缓存区（带 TTL）。
func (s *SQLiteStore) PutCache(ctx context.Context, e memory.Entry, ttlSeconds int64) error {
	tagsJSON, _ := json.Marshal(e.Tags)
	expires := time.Now().Add(time.Duration(ttlSeconds) * time.Second).Format("2006-01-02 15:04:05")

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kb_cache (content, source, tags, source_hash, importance, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.Content, e.Source, tagsJSON, e.SourceHash, e.Importance, expires)
	return err
}

// Delete 删除。
func (s *SQLiteStore) Delete(ctx context.Context, id int64, tier memory.Tier) error {
	table := tierTable(tier)
	// #nosec G201 -- table 来自 tierTable() 白名单（仅 kb_store/kb_cache），非用户输入
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), id)
	return err
}

// tierTable 返回对应层级的表名。
func tierTable(tier memory.Tier) string {
	if tier == memory.TierCache {
		return "kb_cache"
	}
	return "kb_store"
}

// GetIDByHash 按 source_hash 查条目 ID（幂等摄入/向量关联用）。
// 未找到返回 sql.ErrNoRows。
func (s *SQLiteStore) GetIDByHash(ctx context.Context, sourceHash string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM kb_store WHERE source_hash = ?", sourceHash).Scan(&id)
	return id, err
}

// parseTags 解析 JSON 数组 tags。
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		// 兼容空格分隔
		return strings.Fields(s)
	}
	return tags
}
