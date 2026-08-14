// Package storage 提供 SQLite 存储实现。
// 驱动：modernc.org/sqlite（纯 Go，零 CGO，单文件二进制红线关键）。
// 依据：设计阶段第 1 项 + go-sqlite-bench 2026 实测（性能接近 mattn，跨平台无 CGO）。
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec" // sqlite-vec 向量扩展（原生支持，零 CGO）

	"github.com/Black0Bag/minibox/internal/config"
)

// Open 打开（或创建）SQLite 数据库并应用迁移。
func Open(cfg config.DatabaseConfig) (*sql.DB, error) {
	// 确保数据目录存在
	if dir := filepath.Dir(cfg.Path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}

	// 打开数据库（modernc 驱动，DSN 参数）
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(ON)",
		cfg.Path, cfg.BusyTimeout.Milliseconds())

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 连接池设置（SQLite 单写者，串行访问）
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	// 健康检查
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连通性检查失败: %w", err)
	}

	// 应用迁移
	migrator := NewMigrator(db)
	if err := migrator.Migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
