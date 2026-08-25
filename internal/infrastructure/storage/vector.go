package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// vectorJSON 把 float32 向量序列化为 vec0 MATCH 需要的 JSON 字符串。
// 维度校验使用存储实例的 vecDim（配置接入，见 NewSQLiteStore）。
func (s *SQLiteStore) vectorJSON(vec []float32) (string, error) {
	if len(vec) != s.vecDim {
		return "", fmt.Errorf("向量维度 %d 与索引 %d 不一致（检查 cfg.embedding.dimensions）", len(vec), s.vecDim)
	}
	b, err := json.Marshal(vec)
	if err != nil {
		return "", fmt.Errorf("向量序列化失败: %w", err)
	}
	return string(b), nil
}

// Embed 写入/更新条目向量（编译管道 embedding 后调用）。
// 幂等：vec0 主键 doc_id + tier 唯一，重复写入覆盖（INSERT OR REPLACE）。
func (s *SQLiteStore) Embed(ctx context.Context, id int64, tier memory.Tier, vec []float32) error {
	queryVec, err := s.vectorJSON(vec)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO kb_vec (doc_id, tier, embedding)
		VALUES (?, ?, ?)`,
		id, string(tier), queryVec)
	if err != nil {
		return fmt.Errorf("写入向量失败 id=%d: %w", id, err)
	}
	return nil
}

// RemoveEmbedding 删除条目向量（内容回滚/重建时）。
func (s *SQLiteStore) RemoveEmbedding(ctx context.Context, id int64, tier memory.Tier) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM kb_vec WHERE doc_id = ? AND tier = ?", id, string(tier))
	if err != nil {
		return fmt.Errorf("删除向量失败 id=%d: %w", id, err)
	}
	return nil
}

// searchVec KNN 向量检索（vec0，必须带 k 约束）。
// 返回按距离升序的前 TopK 条，JOIN 主表取内容。
func (s *SQLiteStore) searchVec(ctx context.Context, tier memory.Tier, q memory.SearchQuery) ([]memory.Hit, error) {
	if len(q.QueryVector) == 0 {
		return nil, nil
	}
	queryVec, err := s.vectorJSON(q.QueryVector)
	if err != nil {
		return nil, err
	}

	topK := q.TopK
	if topK <= 0 {
		topK = 10
	}
	table := tierTable(tier)

	// vec0 KNN：MATCH + k 约束（v0.1.9 实测要求 k，不能只 ORDER BY LIMIT）。
	// tier 是 vec0 元数据列，可参与 WHERE 过滤。
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT v.doc_id, v.distance,
		       kb.id, kb.content, kb.source, kb.tags, kb.source_hash,
		       kb.importance, kb.access_count, kb.created_at
		FROM kb_vec v
		JOIN %s kb ON kb.id = v.doc_id
		WHERE v.embedding MATCH ? AND v.k = ? AND v.tier = ?
		ORDER BY v.distance`, table),
		queryVec, topK, string(tier))
	if err != nil {
		// vec0 不可用时降级（如扩展未加载）
		return nil, fmt.Errorf("向量检索失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []memory.Hit
	for rows.Next() {
		var h memory.Hit
		var tags, docID string
		var dist float64
		if err := rows.Scan(&docID, &dist,
			&h.ID, &h.Content, &h.Source, &tags, &h.SourceHash,
			&h.Importance, &h.AccessCount, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Tags = parseTags(tags)
		h.Tier = tier
		h.MatchType = "vec"
		h.Score = dist // 向量距离（越小越近）
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// rrfFuse 用 RRF（Reciprocal Rank Fusion）融合 FTS5 和向量两路结果。
// RRF 公式：score = Σ 1/(k + rank)，k=60（业界标准，vstash/mika 实证）。
// 去重：同一 doc 两路都有则合并分数；单路结果保留原 match_type。
func rrfFuse(vecHits, ftsHits []memory.Hit, topK int) []memory.Hit {
	const rrfK = 60
	score := make(map[int64]float64)
	order := make([]int64, 0, len(vecHits)+len(ftsHits))
	seen := make(map[int64]bool)

	// 向量路（按距离升序 = rank 升序）
	for i, h := range vecHits {
		score[h.ID] += 1.0 / (rrfK + float64(i+1))
		if !seen[h.ID] {
			seen[h.ID] = true
			order = append(order, h.ID)
		}
	}
	// FTS5 路（bm25 降序排序）
	for i, h := range ftsHits {
		score[h.ID] += 1.0 / (rrfK + float64(i+1))
		if !seen[h.ID] {
			seen[h.ID] = true
			order = append(order, h.ID)
		}
	}

	// 合并条目内容（优先取向量路，保精度）
	byID := make(map[int64]memory.Hit, len(order))
	fromVec := make(map[int64]bool, len(vecHits))
	fromFTS := make(map[int64]bool, len(ftsHits))
	for _, h := range vecHits {
		byID[h.ID] = h
		fromVec[h.ID] = true
	}
	for _, h := range ftsHits {
		if _, ok := byID[h.ID]; !ok {
			byID[h.ID] = h
		}
		fromFTS[h.ID] = true
	}

	out := make([]memory.Hit, 0, len(order))
	for _, id := range order {
		h := byID[id]
		h.Score = score[id]
		switch {
		case fromVec[id] && fromFTS[id]:
			h.MatchType = "both"
		case fromVec[id]:
			h.MatchType = "vec"
		default:
			h.MatchType = "fts"
		}
		out = append(out, h)
	}
	// 截断 topK
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// hybridSearch 混合检索（Hybrid）：向量 KNN + FTS5 → RRF 融合。
// 任一路失败时降级到另一路（三级降级实证）。
func (s *SQLiteStore) hybridSearch(ctx context.Context, tier memory.Tier, q memory.SearchQuery) ([]memory.Hit, error) {
	topK := q.TopK
	if topK <= 0 {
		topK = 10
	}

	var vecHits, ftsHits []memory.Hit
	var vecErr, ftsErr error

	if len(q.QueryVector) > 0 {
		vecHits, vecErr = s.searchVec(ctx, tier, q)
	}
	ftsHits, ftsErr = s.searchFTS(ctx, tierTable(tier), q)

	// 两路都失败 → 错误
	if vecErr != nil && ftsErr != nil {
		return nil, fmt.Errorf("混合检索失败: vec=%v fts=%v", vecErr, ftsErr)
	}
	// 向量路失败 → 只用 FTS5
	if vecErr != nil {
		return ftsHits, nil
	}
	// FTS5 失败 → 只用向量
	if ftsErr != nil {
		return vecHits, nil
	}
	// 两路都有 → RRF 融合
	if len(vecHits) > 0 || len(ftsHits) > 0 {
		return rrfFuse(vecHits, ftsHits, topK), nil
	}
	return nil, nil
}
