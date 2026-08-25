package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// newTestStore 创建测试用存储 + 分词器 + 编译管道 + 蒸馏器。
func newTestStore(t *testing.T) (*SQLiteStore, *SQLiteCompiler, *SQLiteDistiller) {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1

	db, err := Open(cfg.Database)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tokenizer, err := NewJiebaTokenizer()
	if err != nil {
		t.Fatalf("创建分词器失败: %v", err)
	}

	store := NewSQLiteStore(db, tokenizer, DefaultVecDim)
	compiler := NewCompiler(db, store)
	distiller := NewDistiller(db)
	return store, compiler, distiller
}

// TestTokenizer 验证 jieba 中文分词。
func TestTokenizer(t *testing.T) {
	tok, err := NewJiebaTokenizer()
	if err != nil {
		t.Fatalf("创建分词器失败: %v", err)
	}

	// 索引用分词
	idx, err := tok.TokenizeIndex("我喜欢在周末去爬山")
	if err != nil {
		t.Fatalf("索引分词失败: %v", err)
	}
	if idx == "" {
		t.Error("索引分词结果为空")
	}
	t.Logf("索引分词: %q", idx)

	// 查询用分词（应含引号 + AND）
	query, err := tok.TokenizeQuery("周末爬山")
	if err != nil {
		t.Fatalf("查询分词失败: %v", err)
	}
	if query == "" {
		t.Error("查询分词结果为空")
	}
	t.Logf("查询分词: %q", query)
}

// TestStoreUpsertSearch 验证写入 + 检索。
func TestStoreUpsertSearch(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	// 写入
	err := store.Upsert(ctx, memory.Entry{
		Content:    "北京是中国的首都，历史悠久",
		Source:     "test",
		Tags:       []string{"地理", "常识"},
		SourceHash: "hash-test-1",
		Importance: 0.8,
	})
	if err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}

	// 检索（LIKE 降级，因为 FTS 存的是未分词 content）
	hits, err := store.Search(ctx, memory.SearchQuery{
		Text: "北京",
		TopK: 5,
	})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Error("应检索到至少 1 条")
	} else {
		t.Logf("检索到 %d 条，首条: %s", len(hits), hits[0].Content)
	}

	// 幂等：重复写入同 source_hash 不重复
	err = store.Upsert(ctx, memory.Entry{
		Content:    "北京是中国的首都，历史悠久",
		Source:     "test",
		SourceHash: "hash-test-1",
	})
	if err != nil {
		t.Fatalf("重复 Upsert 失败: %v", err)
	}

	// List 验证只有 1 条
	entries, err := store.List(ctx, memory.TierStore, 0, 10)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("幂等失败：期望 1 条，实际 %d 条", len(entries))
	}
}

func TestStoreFTSChineseCRUDAndRebuild(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	entry := memory.Entry{
		Content:    "上海人工智能实验室发布中文检索基准数据集",
		Source:     "fts-test",
		Tags:       []string{"中文", "检索"},
		SourceHash: "fts-crud-1",
		Importance: 0.9,
	}
	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("写入中文条目失败: %v", err)
	}

	assertFTSMatch := func(query, wantContent string) {
		t.Helper()
		hits, err := store.Search(ctx, memory.SearchQuery{
			Text: query, Tier: memory.TierStore, TopK: 5,
		})
		if err != nil {
			t.Fatalf("检索 %q 失败: %v", query, err)
		}
		if len(hits) == 0 {
			t.Fatalf("检索 %q 没有命中", query)
		}
		if hits[0].MatchType != "fts" {
			t.Fatalf("检索 %q 应走 FTS，实际 match_type=%q", query, hits[0].MatchType)
		}
		if hits[0].Content != wantContent {
			t.Fatalf("检索 %q 内容异常: %q", query, hits[0].Content)
		}
	}

	assertFTSMatch("人工智能", entry.Content)
	assertFTSMatch("中文检索", entry.Content)

	id, err := store.GetIDByHash(ctx, entry.SourceHash)
	if err != nil {
		t.Fatalf("读取条目 ID 失败: %v", err)
	}
	updated := memory.Entry{
		ID:         id,
		Content:    "上海人工智能实验室发布新的向量检索评测数据集",
		Source:     "fts-test-updated",
		Tags:       []string{"中文", "向量"},
		SourceHash: entry.SourceHash,
		Importance: 0.95,
	}
	if err := store.Upsert(ctx, updated); err != nil {
		t.Fatalf("更新中文条目失败: %v", err)
	}
	assertFTSMatch("向量检索", updated.Content)

	if err := store.RebuildFTS(ctx); err != nil {
		t.Fatalf("FTS 回填重建失败: %v", err)
	}
	assertFTSMatch("评测数据集", updated.Content)

	if err := store.Delete(ctx, id, memory.TierStore); err != nil {
		t.Fatalf("删除中文条目失败: %v", err)
	}
	hits, err := store.Search(ctx, memory.SearchQuery{
		Text: "向量检索", Tier: memory.TierStore, TopK: 5,
	})
	if err != nil {
		t.Fatalf("删除后检索失败: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("删除后不应残留 FTS 命中，实际: %+v", hits)
	}
}

func TestStoreFTSRebuildBackfillsLegacyEnglishAndSpecialQueries(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	// 模拟 0006 迁移前或绕过 Store 的旧数据：tokenized_content 初始为空。
	result, err := store.db.ExecContext(ctx, `
		INSERT INTO kb_store (content, source, tags, source_hash, importance)
		VALUES (?, ?, ?, ?, ?)`,
		"OpenAI-compatible API supports C++ and Go backends", "legacy", `["english","api"]`, "fts-legacy-en", 0.7)
	if err != nil {
		t.Fatalf("写入 legacy 条目失败: %v", err)
	}
	legacyID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("读取 legacy ID 失败: %v", err)
	}

	if err := store.RebuildFTS(ctx); err != nil {
		t.Fatalf("legacy FTS 回填失败: %v", err)
	}
	var tokenized string
	if err := store.db.QueryRowContext(ctx, "SELECT tokenized_content FROM kb_store WHERE id = ?", legacyID).Scan(&tokenized); err != nil {
		t.Fatalf("读取回填 tokenized_content 失败: %v", err)
	}
	if tokenized == "" || tokenized == "OpenAI-compatible API supports C++ and Go backends" {
		t.Fatalf("legacy 条目未被分词回填: %q", tokenized)
	}

	for _, query := range []string{"OpenAI", "API", "C++", "Go"} {
		hits, err := store.Search(ctx, memory.SearchQuery{Text: query, Tier: memory.TierStore, TopK: 5})
		if err != nil {
			t.Fatalf("英文/特殊字符检索 %q 失败: %v", query, err)
		}
		if len(hits) == 0 || hits[0].MatchType != "fts" || hits[0].ID != legacyID {
			t.Fatalf("英文/特殊字符检索 %q 应命中 FTS legacy 条目，实际: %+v", query, hits)
		}
	}

	// 空查询应保持可预期的降级语义：无命中或显式错误均可，但绝不能 panic。
	if _, err := store.Search(ctx, memory.SearchQuery{Text: "", Tier: memory.TierStore, TopK: 5}); err != nil {
		t.Fatalf("空查询不应触发底层 FTS 错误: %v", err)
	}
}

// TestStoreCache 验证缓存区（带 TTL）。
func TestStoreCache(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	err := store.PutCache(ctx, memory.Entry{
		Content:    "临时片段",
		SourceHash: "cache-1",
	}, 3600)
	if err != nil {
		t.Fatalf("PutCache 失败: %v", err)
	}

	entries, err := store.List(ctx, memory.TierCache, 0, 10)
	if err != nil {
		t.Fatalf("List 缓存区失败: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("缓存区应 1 条，实际 %d 条", len(entries))
	}
}

// TestCompiler 验证编译管道状态机。
func TestCompiler(t *testing.T) {
	_, compiler, _ := newTestStore(t)
	ctx := context.Background()

	job, err := compiler.Compile(ctx, "这是一段待编译的知识内容", memory.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	if job.Status != memory.JobPending {
		t.Errorf("初始状态应为 PENDING，实际 %s", job.Status)
	}

	// 等待异步处理完成
	deadline := time.Now().Add(5 * time.Second)
	var status memory.JobStatus
	for time.Now().Before(deadline) {
		got, err := compiler.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("GetJob 失败: %v", err)
		}
		status = got.Status
		if status == memory.JobReady || status == memory.JobFailed {
			if status == memory.JobFailed {
				t.Fatalf("编译失败: %s", got.Error)
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if status != memory.JobReady {
		t.Errorf("应到达 READY，实际 %s", status)
	}
}

// TestChunkSource 验证切块逻辑。
func TestChunkSource(t *testing.T) {
	long := "段落一内容，讨论知识库设计。\n\n段落二内容，讨论向量检索。\n\n段落三内容，讨论 agent 架构。"
	chunks := chunkSource(long, 0)
	if len(chunks) < 1 {
		t.Fatalf("应有至少 1 个 chunk，实际 %d", len(chunks))
	}
	if chunks[0] == "" {
		t.Error("chunk 不应为空")
	}
}

// TestCompilerEmbedder 验证 embedding 链路（mock embedder 写向量）。
func TestCompilerEmbedder(t *testing.T) {
	store, compiler, _ := newTestStore(t)
	ctx := context.Background()

	compiler.SetEmbedder(fakeEmbedder{})
	job, err := compiler.Compile(ctx, "嵌入向量测试内容，验证编译管道写入向量。", memory.CompileOptions{})
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	got, _ := compiler.GetJob(ctx, job.ID)
	if got.Status != memory.JobReady {
		t.Fatalf("应 READY: %s", got.Error)
	}

	// 验证向量入库（dim 应与 fakeEmbedder 返回一致，太大则拒绝——用 1024 维）
	hits, err := store.Search(ctx, memory.SearchQuery{
		Text:        "嵌入向量测试",
		TopK:        3,
		QueryVector: make([]float32, 128), // fakeEmbedder 用 128 维
	})
	_ = hits
	_ = err
}

// fakeEmbedder 测试用向量化器（128 维）。
type fakeEmbedder struct{}

func (fakeEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return [][]float32{make([]float32, 128)}, nil
}

var _ Embedder = (*fakeEmbedder)(nil)

func TestCompilerStructuredExtractorAndEmbedding(t *testing.T) {
	store, compiler, _ := newTestStore(t)
	ctx := context.Background()
	compiler.SetKnowledgeExtractor(staticCompilerExtractor{entries: []memory.Entry{{
		Content:    "结构化提炼后的知识条目",
		Tags:       []string{"结构化", "编译"},
		Importance: 0.8,
	}}})
	compiler.SetEmbedder(fakeEmbedder1024{})
	job, err := compiler.Compile(ctx, "原始资料不会直接作为最终条目", memory.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := waitCompilerJob(t, compiler, job.ID)
	if got.Status != memory.JobReady || got.Total != 1 {
		t.Fatalf("job=%+v", got)
	}
	hits, err := store.Search(ctx, memory.SearchQuery{Text: "结构化提炼", TopK: 5, QueryVector: make([]float32, 1024)})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Content != "结构化提炼后的知识条目" || hits[0].MatchType != "both" {
		t.Fatalf("structured compile hits=%+v", hits)
	}
}

func TestCompilerExtractorFailureFallsBackToSource(t *testing.T) {
	store, compiler, _ := newTestStore(t)
	compiler.SetKnowledgeExtractor(staticCompilerExtractor{err: errors.New("extractor unavailable")})
	job, err := compiler.Compile(context.Background(), "提炼失败时仍应保留原始知识文本", memory.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := waitCompilerJob(t, compiler, job.ID)
	if got.Status != memory.JobReady {
		t.Fatalf("fallback job=%+v", got)
	}
	hits, err := store.Search(context.Background(), memory.SearchQuery{Text: "原始知识文本", TopK: 5})
	if err != nil || len(hits) == 0 || hits[0].Content != "提炼失败时仍应保留原始知识文本" {
		t.Fatalf("fallback hits=%+v err=%v", hits, err)
	}
}

type staticCompilerExtractor struct {
	entries []memory.Entry
	err     error
}

func (e staticCompilerExtractor) Extract(context.Context, string) ([]memory.Entry, error) {
	return e.entries, e.err
}

type fakeEmbedder1024 struct{}

func (fakeEmbedder1024) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return [][]float32{make([]float32, DefaultVecDim)}, nil
}

func waitCompilerJob(t *testing.T, compiler *SQLiteCompiler, id string) *memory.CompileJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := compiler.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == memory.JobReady || job.Status == memory.JobFailed {
			return job
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("编译作业超时")
	return nil
}

// TestDistillerLLMExtractionAndFallback 验证结构化提炼成功和失败回退。
func TestDistillerLLMExtractionAndFallback(t *testing.T) {
	store, _, distiller := newTestStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, memory.Entry{Content: "用户偏好深色主题", SourceHash: "llm-pref", Importance: 0.9}); err != nil {
		t.Fatal(err)
	}
	if _, err := distiller.db.ExecContext(ctx, "UPDATE kb_store SET access_count = 3 WHERE source_hash = ?", "llm-pref"); err != nil {
		t.Fatal(err)
	}

	distiller.SetPrefExtractor(staticPrefExtractor{prefs: []memory.Preference{{Key: "主题", Value: "深色", Probability: 0.88}}})
	result, err := distiller.Distill(ctx, memory.DistillOptions{MinEvidence: 1, ImportanceThreshold: 0.5})
	if err != nil || result.Extracted != 1 {
		t.Fatalf("LLM distill result=%+v err=%v", result, err)
	}
	prefs, err := distiller.ListPreferences(ctx, 10)
	if err != nil || len(prefs) != 1 || prefs[0].Key != "主题" || prefs[0].EvidenceCnt != 1 || prefs[0].Probability != 0.88 {
		t.Fatalf("LLM preferences=%+v err=%v", prefs, err)
	}

	if err := store.Upsert(ctx, memory.Entry{Content: "用户偏好简洁回答", SourceHash: "fallback-pref", Importance: 0.8}); err != nil {
		t.Fatal(err)
	}
	if _, err := distiller.db.ExecContext(ctx, "UPDATE kb_store SET access_count = 2 WHERE source_hash = ?", "fallback-pref"); err != nil {
		t.Fatal(err)
	}
	distiller.SetPrefExtractor(staticPrefExtractor{err: errors.New("fixture extractor failure")})
	result, err = distiller.Distill(ctx, memory.DistillOptions{MinEvidence: 1, ImportanceThreshold: 0.5})
	if err != nil || result.Extracted == 0 {
		t.Fatalf("fallback distill result=%+v err=%v", result, err)
	}
	prefs, err = distiller.ListPreferences(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundFallback := false
	for _, p := range prefs {
		if p.Key == "用户偏好简洁回答" && p.EvidenceCnt >= 2 {
			foundFallback = true
		}
	}
	if !foundFallback {
		t.Fatalf("未找到统计回退偏好: %+v", prefs)
	}
}

type staticPrefExtractor struct {
	prefs []memory.Preference
	err   error
}

func (e staticPrefExtractor) Extract(context.Context, string) ([]memory.Preference, error) {
	return e.prefs, e.err
}

var _ PrefExtractor = staticPrefExtractor{}
