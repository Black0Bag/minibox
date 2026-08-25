package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// newTestVecStore 创建带向量能力的测试存储。
func newTestVecStore(t *testing.T) *SQLiteStore {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "vec.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1

	db, err := Open(cfg.Database)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tok, err := NewJiebaTokenizer()
	if err != nil {
		t.Fatalf("创建分词器失败: %v", err)
	}
	return NewSQLiteStore(db, tok, DefaultVecDim)
}

// vecFor 构造一个可预测的 1024 维向量：第 i 维 = value，其余 0。
// 用于 KNN 距离可断言。
func vecFor(i int, value float32) []float32 {
	v := make([]float32, DefaultVecDim)
	if i >= 0 && i < DefaultVecDim {
		v[i] = value
	}
	return v
}

// TestEmbedAndSearchVec 向量写入 + KNN 检索 + 距离排序。
func TestEmbedAndSearchVec(t *testing.T) {
	store := newTestVecStore(t)
	ctx := context.Background()

	// 写入两条知识 + 向量
	if err := store.Upsert(ctx, memory.Entry{
		Content: "苹果是红色的水果", Source: "s1", SourceHash: "apple", Importance: 0.8,
	}); err != nil {
		t.Fatalf("Upsert1 失败: %v", err)
	}
	if err := store.Upsert(ctx, memory.Entry{
		Content: "香蕉是黄色的水果", Source: "s2", SourceHash: "banana", Importance: 0.5,
	}); err != nil {
		t.Fatalf("Upsert2 失败: %v", err)
	}

	// 拿回 id
	id1, err := store.GetIDByHash(ctx, "apple")
	if err != nil {
		t.Fatalf("取 apple id 失败: %v", err)
	}
	id2, err := store.GetIDByHash(ctx, "banana")
	if err != nil {
		t.Fatalf("取 banana id 失败: %v", err)
	}

	// 写入向量：id1 指向 index 5, id2 指向 index 900（距离远）
	if err := store.Embed(ctx, id1, memory.TierStore, vecFor(5, 1)); err != nil {
		t.Fatalf("Embed1 失败: %v", err)
	}
	if err := store.Embed(ctx, id2, memory.TierStore, vecFor(900, 1)); err != nil {
		t.Fatalf("Embed2 失败: %v", err)
	}

	// KNN：查询向量 = id1 的向量（index 5）→ 应命中 id1 优先
	hits, err := store.Search(ctx, memory.SearchQuery{
		Text:        "苹果",
		Tier:        memory.TierStore,
		TopK:        10,
		QueryVector: vecFor(5, 1),
	})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("向量检索应返回结果")
	}
	// 距离最小 = 最相似；id1 的向量应排第一（RRF 融合后分数，非原始距离）
	if hits[0].ID != id1 {
		t.Errorf("最相似应为 id1，实际 id=%d", hits[0].ID)
	}
	// id1 分数应高于 id2（id1 向量与查询完全匹配）
	if len(hits) > 1 && hits[1].ID == id2 {
		if hits[0].Score <= hits[1].Score {
			t.Errorf("id1 分数应高于 id2：%f vs %f", hits[0].Score, hits[1].Score)
		}
	}
}

// TestEmbedWrongDim 维度不一致应拒绝。
func TestEmbedWrongDim(t *testing.T) {
	store := newTestVecStore(t)
	err := store.Embed(context.Background(), 1, memory.TierStore, []float32{1, 2, 3})
	if err == nil {
		t.Fatal("维度错误应返回错误")
	}
}

// TestRemoveEmbedding 删除向量。
func TestRemoveEmbedding(t *testing.T) {
	store := newTestVecStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, memory.Entry{
		Content: "测试内容", SourceHash: "h1", Importance: 0.5,
	}); err != nil {
		t.Fatal(err)
	}
	id, err := store.GetIDByHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Embed(ctx, id, memory.TierStore, vecFor(10, 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveEmbedding(ctx, id, memory.TierStore); err != nil {
		t.Fatalf("RemoveEmbedding 失败: %v", err)
	}

	// 删除后 hybrid 无向量结果，降级 FTS5 仍可命中（降级健壮性）
	hits, err := store.Search(ctx, memory.SearchQuery{
		Text: "测试", Tier: memory.TierStore, TopK: 10, QueryVector: vecFor(10, 1),
	})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Error("删除向量后 FTS5/LIKE 降级应仍可命中，实际 0 条")
	}
	for _, h := range hits {
		if h.MatchType == "vec" || h.MatchType == "hybrid" {
			t.Errorf("删除向量后不应有 vec 级别命中，match_type=%s", h.MatchType)
		}
	}
}

// TestDeleteStoreSyncsVec 删除 kb_store 条目应同步清向量（触发器）。
func TestDeleteStoreSyncsVec(t *testing.T) {
	store := newTestVecStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, memory.Entry{
		Content: "将被删除", SourceHash: "del", Importance: 0.5,
	}); err != nil {
		t.Fatal(err)
	}
	id, err := store.GetIDByHash(ctx, "del")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Embed(ctx, id, memory.TierStore, vecFor(20, 1)); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(ctx, id, memory.TierStore); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	// 删除后向量应被触发器清掉
	var cnt int
	if err := store.db.QueryRow(
		"SELECT count(*) FROM kb_vec WHERE doc_id = ?", id).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Errorf("删除主表后向量应同步清除，实际 %d 条", cnt)
	}
}

// TestSearchNoVectorFallsBack 无查询向量时降级 FTS5→LIKE（三级降级）。
func TestSearchNoVectorFallsBack(t *testing.T) {
	store := newTestVecStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, memory.Entry{
		Content: "北京是中国的首都", SourceHash: "bj", Importance: 0.9,
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := store.Search(ctx, memory.SearchQuery{
		Text: "北京", Tier: memory.TierStore, TopK: 10,
	})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("无向量时应通过 FTS5/LIKE 返回结果")
	}
}

func TestSearchDefaultsEmptyTierToStore(t *testing.T) {
	store := newTestVecStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, memory.Entry{Content: "默认层级检索测试", SourceHash: "default-tier"}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(ctx, memory.SearchQuery{Text: "默认层级"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Tier != memory.TierStore {
		t.Fatalf("空 Tier 应默认检索 store，实际: %+v", hits)
	}
}
func TestRRFFusePreservesSingleRouteMatchType(t *testing.T) {
	vecOnly := rrfFuse([]memory.Hit{{Entry: memory.Entry{ID: 1}}}, nil, 10)
	if len(vecOnly) != 1 || vecOnly[0].MatchType != "vec" {
		t.Fatalf("向量单路 match_type 异常: %+v", vecOnly)
	}
	ftsOnly := rrfFuse(nil, []memory.Hit{{Entry: memory.Entry{ID: 2}}}, 10)
	if len(ftsOnly) != 1 || ftsOnly[0].MatchType != "fts" {
		t.Fatalf("FTS 单路 match_type 异常: %+v", ftsOnly)
	}
}

func TestRRFFuse(t *testing.T) {
	vecHits := []memory.Hit{
		{Entry: memory.Entry{ID: 1}, Score: 0.1},
		{Entry: memory.Entry{ID: 3}, Score: 0.5},
	}
	ftsHits := []memory.Hit{
		{Entry: memory.Entry{ID: 3}, Score: -5},
		{Entry: memory.Entry{ID: 2}, Score: -3},
	}
	fused := rrfFuse(vecHits, ftsHits, 10)
	if len(fused) != 3 {
		t.Fatalf("去重后应有 3 条，实际 %d", len(fused))
	}
	// id3 两路都有 → 分数应最高
	maxID, maxScore := int64(0), float64(0)
	for _, h := range fused {
		if h.Score > maxScore {
			maxID, maxScore = h.ID, h.Score
		}
	}
	if maxID != 3 {
		t.Errorf("双路命中的 id3 分数应最高，实际 maxID=%d", maxID)
	}
}
