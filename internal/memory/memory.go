package memory

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// Store 知识库存储（SQLite WAL，双区制：cache 缓存区 / store 存储区）
type Store struct {
	db      *sql.DB
	dataDir string
}

// Open 打开（或创建）知识库数据库
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, "minibox.db")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s: %w", path, err)
	}
	// 连接池：写串行（SQLite 单写者），读并行
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	s := &Store{db: db, dataDir: dataDir}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// migrate 轻量 schema 迁移（基于 PRAGMA user_version，有序迁移数组）
func (s *Store) migrate() error {
	migrations := []func(*sql.Tx) error{
		createSchemaV1,
		createSchemaV2,
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("读取 schema 版本: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("开始迁移事务: %w", err)
		}
		if err := migrations[i](tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行迁移 v%d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("更新 schema 版本: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移: %w", err)
		}
		slog.Info("schema 迁移完成", "version", i+1)
	}
	return nil
}

// createSchemaV1 知识库 v1 表结构（Q-01 双区制默认设计）
func createSchemaV1(tx *sql.Tx) error {
	stmts := []string{
		// 知识条目（双区制：zone=cache 缓存区 / store 存储区）
		`CREATE TABLE IF NOT EXISTS entries (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			zone        TEXT NOT NULL DEFAULT 'cache',
			type        TEXT NOT NULL DEFAULT 'text',
			title       TEXT NOT NULL DEFAULT '',
			content     TEXT NOT NULL,
			tags        TEXT NOT NULL DEFAULT '',
			source      TEXT NOT NULL DEFAULT '',
			probability REAL NOT NULL DEFAULT 1.0,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_zone ON entries(zone)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_updated ON entries(updated_at)`,
		// FTS5 全文索引（trigram 中文分词，路线图验证结论；普通表支持标准增删改 + bm25）
		`CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
			content, title, tags,
			tokenize='trigram'
		)`,
		// 向量表结构（sqlite-vec，M2 先建结构，embedding 生成后续接入）
		`CREATE TABLE IF NOT EXISTS entry_vectors (
			entry_id   INTEGER PRIMARY KEY,
			dimensions INTEGER NOT NULL,
			vector     BLOB NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("建表失败: %w\nSQL: %s", err, s)
		}
	}
	return nil
}

// DB 返回底层连接（内部使用）
func (s *Store) DB() *sql.DB { return s.db }

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// createSchemaV2 编译管道任务表（M2-2）
func createSchemaV2(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS compile_tasks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			source_type TEXT NOT NULL,          -- url / text
			content     TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'pending', -- pending/running/done/failed
			result_id   INTEGER,                -- 编译产出的条目 ID
			error_msg   TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("建表失败(v2): %w", err)
		}
	}
	return nil
}
