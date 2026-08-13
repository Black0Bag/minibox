package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// migrationsFS 嵌入迁移 SQL 文件（go:embed）。
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator 管理数据库 schema 迁移。
// 方案：PRAGMA user_version + go:embed SQL 文件，按文件名顺序执行。
// 依据：PRD 第 1 项决策——单二进制场景最轻，不用 golang-migrate。
type Migrator struct {
	db *sql.DB
}

// NewMigrator 创建迁移器。
func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Migrate 执行所有未应用的迁移。
// 每个迁移在一个事务中执行，成功后更新 user_version。
func (m *Migrator) Migrate() error {
	current, err := m.currentVersion()
	if err != nil {
		return fmt.Errorf("读取当前版本: %w", err)
	}

	migrations, err := m.list()
	if err != nil {
		return fmt.Errorf("读取迁移文件: %w", err)
	}

	applied := 0
	for _, mig := range migrations {
		if mig.version <= current {
			continue
		}
		if err := m.apply(mig); err != nil {
			return fmt.Errorf("应用迁移 %s 失败: %w", mig.name, err)
		}
		applied++
	}

	if applied == 0 {
		// 无待应用迁移
		return nil
	}
	return nil
}

// migration 表示一个迁移。
type migration struct {
	version int
	name    string
	sql     string
}

// currentVersion 读取当前 schema 版本（PRAGMA user_version）。
func (m *Migrator) currentVersion() (int, error) {
	var v int
	if err := m.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// list 列出所有迁移（按版本号排序）。
func (m *Migrator) list() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// 文件名格式：NNNN_name.sql
		verStr := strings.SplitN(e.Name(), "_", 2)[0]
		ver, err := strconv.Atoi(verStr)
		if err != nil {
			continue // 跳过非标准命名
		}
		data, err := migrationsFS.ReadFile(filepath.Join("migrations", e.Name()))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration{
			version: ver,
			name:    e.Name(),
			sql:     string(data),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

// apply 在事务中应用单个迁移。
func (m *Migrator) apply(mig migration) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 执行迁移 SQL
	if _, err := tx.Exec(mig.sql); err != nil {
		return fmt.Errorf("%s: %w", mig.name, err)
	}

	// 更新版本号
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", mig.version)); err != nil {
		return err
	}

	return tx.Commit()
}
