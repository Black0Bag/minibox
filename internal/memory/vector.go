package memory

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// 默认向量维度（对应 NVIDIA nemotron-3-embed-1b；换模型需重建向量表，N-11）
const DefaultVecDim = 2048

// VectorHit 向量检索命中
type VectorHit struct {
	EntryID  int64
	Distance float64
}

// EnsureVecTable 确保 vec0 虚拟表存在（维度固定；换 embedding 模型需 drop 重建）
func (s *Store) EnsureVecTable(dim int) error {
	if dim <= 0 {
		dim = DefaultVecDim
	}
	_, err := s.db.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS entry_vec USING vec0(entry_id INTEGER PRIMARY KEY, embedding float[%d])`, dim))
	if err != nil {
		return fmt.Errorf("创建向量表: %w", err)
	}
	return nil
}

// SetVector 为条目写入向量（sqlite-vec：vec_f32 接受 JSON 数组字符串）
func (s *Store) SetVector(entryID int64, vec []float32) error {
	if err := s.EnsureVecTable(len(vec)); err != nil {
		return err
	}
	b, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("序列化向量: %w", err)
	}
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO entry_vec(entry_id, embedding) VALUES(?, vec_f32(?))`,
		entryID, string(b)); err != nil {
		return fmt.Errorf("写入向量: %w", err)
	}
	return nil
}

// VectorSearch 向量 KNN 检索（余弦距离，越小越相似）
func (s *Store) VectorSearch(vec []float32, limit int) ([]VectorHit, error) {
	if err := s.EnsureVecTable(len(vec)); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	b, err := json.Marshal(vec)
	if err != nil {
		return nil, fmt.Errorf("序列化向量: %w", err)
	}
	q := string(b)
	// sqlite-vec 要求 KNN 查询的 LIMIT 为常量：用 `k = N` 语法（limit 为 int，安全拼接）
	rows, err := s.db.Query(fmt.Sprintf(
		`SELECT entry_id, vec_distance_cosine(embedding, vec_f32(?)) AS d
		FROM entry_vec WHERE embedding MATCH vec_f32(?) AND k = %d ORDER BY d`, limit), q, q)
	if err != nil {
		return nil, fmt.Errorf("向量检索: %w", err)
	}
	defer func() { _ = rows.Close() }()
	hits := []VectorHit{}
	for rows.Next() {
		var h VectorHit
		if err := rows.Scan(&h.EntryID, &h.Distance); err != nil {
			return nil, fmt.Errorf("扫描向量结果: %w", err)
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// rrf 融合分数：score = sum(1/(k+rank))，k=60
const rrfK = 60.0

// hybridFuse 将 FTS 命中与向量命中做 RRF 融合，返回按分数降序的条目 ID 列表
func hybridFuse(ftsIDs map[int64]int, vecIDs map[int64]int) []int64 {
	scores := map[int64]float64{}
	for id, rank := range ftsIDs {
		scores[id] += 1.0 / (rrfK + float64(rank))
	}
	for id, rank := range vecIDs {
		scores[id] += 1.0 / (rrfK + float64(rank))
	}
	type sc struct {
		id    int64
		score float64
	}
	list := make([]sc, 0, len(scores))
	for id, scv := range scores {
		list = append(list, sc{id, scv})
	}
	sort.Slice(list, func(i, j int) bool {
		if math.Abs(list[i].score-list[j].score) < 1e-9 {
			return list[i].id < list[j].id
		}
		return list[i].score > list[j].score
	})
	out := make([]int64, len(list))
	for i, v := range list {
		out[i] = v.id
	}
	return out
}
