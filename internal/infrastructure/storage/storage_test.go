package storage

import (
	"path/filepath"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
)

// TestMigrate 验证迁移引擎能从零建表。
func TestMigrate(t *testing.T) {
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1

	db, err := Open(cfg.Database)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 验证 schema_meta 表存在
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取 user_version 失败: %v", err)
	}
	// 期望 = 已嵌入迁移文件的最大版本号（动态，避免加迁移后忘记更新）
	wantVersion := latestMigrationVersion(t)
	if version != wantVersion {
		t.Errorf("期望 user_version=%d, 实际 %d", wantVersion, version)
	}

	// 验证核心表存在
	for _, table := range []string{"schema_meta", "kb_store", "kb_cache", "kb_fts", "kb_vec", "kb_snapshots", "kb_preferences"} {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type IN ('table','virtual') AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("表 %s 不存在: %v", table, err)
		}
	}

	// 验证触发器存在
	var trig string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='trigger' AND name='kb_store_ai'",
	).Scan(&trig)
	if err != nil {
		t.Errorf("触发器 kb_store_ai 不存在: %v", err)
	}
}

// latestMigrationVersion 返回已嵌入迁移文件的最大版本号。
func latestMigrationVersion(t *testing.T) int {
	t.Helper()
	migs, err := (&Migrator{db: nil}).list()
	if err != nil {
		t.Fatalf("列出迁移失败: %v", err)
	}
	if len(migs) == 0 {
		return 0
	}
	return migs[len(migs)-1].version
}

// TestKbStoreInsert 验证 kb_store 插入 + FTS5 自动同步。
func TestKbStoreInsert(t *testing.T) {
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Database.MaxOpenConns = 1
	cfg.Database.MaxIdleConns = 1

	db, err := Open(cfg.Database)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 插入一条知识
	_, err = db.Exec(
		"INSERT INTO kb_store (content, tags, source_hash) VALUES (?, ?, ?)",
		"北京是中国的首都", "地点,常识", "hash1",
	)
	if err != nil {
		t.Fatalf("插入 kb_store 失败: %v", err)
	}

	// 验证 FTS5 同步（unicode61 tokenizer 按空格分词）
	// 中文整句会被当一个词存，这里用整句前缀查询验证同步机制
	var count int
	err = db.QueryRow(
		"SELECT count(*) FROM kb_fts WHERE kb_fts MATCH ?", `"北京是中国的首都"`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 查询失败: %v", err)
	}
	if count != 1 {
		t.Errorf("FTS5 应匹配 1 条，实际 %d", count)
	}

	// 验证删除同步
	_, err = db.Exec("DELETE FROM kb_store WHERE source_hash = ?", "hash1")
	if err != nil {
		t.Fatalf("删除 kb_store 失败: %v", err)
	}
	err = db.QueryRow(
		"SELECT count(*) FROM kb_fts WHERE kb_fts MATCH ?", `"北京是中国的首都"`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 二次查询失败: %v", err)
	}
	if count != 0 {
		t.Errorf("删除后 FTS5 应匹配 0 条，实际 %d", count)
	}
}
