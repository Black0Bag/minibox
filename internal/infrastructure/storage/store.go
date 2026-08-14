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

// SQLiteStore 基于 SQLite 的知识库存储实现。
// 表：kb_store（存储区）/ kb_cache（缓存区）/ kb_fts（FTS5）/ kb_vec（sqlite-vec）。
type SQLiteStore struct {
	db        *sql.DB
	tokenizer memory.Tokenizer
}

// NewSQLiteStore 创建 SQLite 知识库存储。
func NewSQLiteStore(db *sql.DB, tokenizer memory.Tokenizer) *SQLiteStore {
	return &SQLiteStore{db: db, tokenizer: tokenizer}
}

// Search 混合检索（三级降级：Hybrid→FTS5→LIKE）。
func (s *SQLiteStore) Search(ctx context.Context, q memory.SearchQuery) ([]memory.Hit, error) {
	if q.TopK == 0 {
		q.TopK = 10
	}
	if q.MinScore == 0 {
		q.MinScore = 0.0
	}
	table := tierTable(q.Tier)

	// 1. 尝试 FTS5 关键词检索
	hits, err := s.searchFTS(ctx, table, q)
	if err != nil || len(hits) > 0 {
		return hits, err
	}

	// 2. 降级到 LIKE（FTS5 无结果或不可用）
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
		if h.Score >= q.MinScore {
			hits = append(hits, h)
		}
	}
	return hits, rows.Err()
}

// searchLike 降级：LIKE 子串匹配（索引未就绪/embedding 不可用）。
func (s *SQLiteStore) searchLike(ctx context.Context, table string, q memory.SearchQuery) ([]memory.Hit, error) {
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
	// 预分词（写入和查询同一 pipeline）
	tokenized, err := s.tokenizer.TokenizeIndex(e.Content)
	if err != nil {
		return fmt.Errorf("分词失败: %w", err)
	}
	tagsJSON, _ := json.Marshal(e.Tags)

	// 触发器自动同步 FTS5（用原始 content 还是分词后？）
	// 设计：kb_store.content 存原始文本，分词结果单独存 tokenized 列用于 FTS。
	// 说明：当前 0001_init.sql 的 kb_fts 触发器直接同步 content（未分词）。
	// 为支持 jieba，需要调整：写入时把 tokenized 存到一个 tokenized 列，FTS 触发器读它。
	// 这里先保持简单：content 存原始，FTS 触发器后续迁移改为读 tokenized 列。
	_ = tokenized

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kb_store (content, source, tags, source_hash, importance)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source_hash) DO NOTHING`,
		e.Content, e.Source, tagsJSON, e.SourceHash, e.Importance)
	return err
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
