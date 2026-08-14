package storage

import (
	"context"
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
	t.Cleanup(func() { db.Close() })

	tokenizer, err := NewJiebaTokenizer()
	if err != nil {
		t.Fatalf("创建分词器失败: %v", err)
	}

	store := NewSQLiteStore(db, tokenizer)
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
		t.Errorf("缓存区应 1 条，实际 %d", len(entries))
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
	time.Sleep(100 * time.Millisecond)

	got, err := compiler.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob 失败: %v", err)
	}
	if got.Status != memory.JobReady {
		t.Errorf("应到达 READY，实际 %s（error=%s）", got.Status, got.Error)
	}
}

// TestDistiller 验证蒸馏。
func TestDistiller(t *testing.T) {
	store, _, distiller := newTestStore(t)
	ctx := context.Background()

	// 写入高频条目
	store.Upsert(ctx, memory.Entry{
		Content:    "用户偏好使用深色主题",
		SourceHash: "pref-1",
		Importance: 0.9,
	})

	// 模拟高频访问（直接 update access_count）
	db := distiller.db
	db.Exec("UPDATE kb_store SET access_count = 5 WHERE source_hash = 'pref-1'")

	result, err := distiller.Distill(ctx, memory.DistillOptions{
		MinEvidence:         1,
		ImportanceThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if result.Extracted == 0 {
		t.Error("应蒸馏出至少 1 条偏好")
	}

	prefs, err := distiller.ListPreferences(ctx, 10)
	if err != nil {
		t.Fatalf("ListPreferences 失败: %v", err)
	}
	if len(prefs) == 0 {
		t.Error("偏好列表为空")
	} else {
		t.Logf("偏好: %+v", prefs[0])
	}
}
