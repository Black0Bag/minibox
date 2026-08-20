package storage

import (
	"database/sql"
	"embed"
	"errors"
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
	defer func() { _ = tx.Rollback() }()

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

// ValidateVecDim 校验配置向量维度与 schema_meta 记录的一致性（B24 向量维度全适配）。
// vec0 虚表维度创建时固定（0001/0002：float[1024]），切换 embedding 模型须重建 kb_vec 索引。
// - schema_meta 未记录（首次启动）→ 写入记录，返回 nil。
// - 已记录且与配置不一致 → 返回可操作错误（提示重建索引），防止静默写入错维向量。
func ValidateVecDim(db *sql.DB, dim int) error {
	if dim <= 0 {
		dim = DefaultVecDim
	}
	var recorded sql.NullInt64
	err := db.QueryRow("SELECT embedding_dim FROM schema_meta WHERE id = 1").Scan(&recorded)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("读取 schema_meta 失败: %w", err)
	}
	// 首次：写入维度记录
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.Exec(
			"INSERT INTO schema_meta (id, embedding_dim) VALUES (1, ?) "+
				"ON CONFLICT(id) DO UPDATE SET embedding_dim = excluded.embedding_dim", dim); err != nil {
			return fmt.Errorf("写入 schema_meta 维度失败: %w", err)
		}
		return nil
	}
	// 已记录：比对
	if recorded.Valid && int(recorded.Int64) != dim {
		return fmt.Errorf("向量维度不匹配：schema_meta=%d 配置=%d。"+
			"切换 embedding 模型须重建 kb_vec 索引（DROP 后重建，向量由编译管道重灌）", recorded.Int64, dim)
	}
	return nil
}
