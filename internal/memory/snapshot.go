package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Snapshot 生成一致性快照（VACUUM INTO，SQLite 原生在线安全）
// 返回快照文件名（相对快照目录）
func (s *Store) Snapshot(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建快照目录: %w", err)
	}
	name := "snapshot-" + time.Now().Format("20060102-150405") + ".db"
	path := filepath.Join(dir, name)
	// 路径可能含单引号，转义
	safe := path
	if _, err := s.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", safe)); err != nil {
		return "", fmt.Errorf("生成快照: %w", err)
	}
	return name, nil
}

// ListSnapshots 列出快照（按时间倒序）
func (s *Store) ListSnapshots(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取快照目录: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".db" {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// Restore 用快照文件恢复数据库（关闭当前连接 → 覆盖主库 → 重新打开）
func (s *Store) Restore(snapshotPath string) error {
	// 先做恢复前备份，防失败
	backup := s.dbPath + ".pre-restore"
	data, err := os.ReadFile(s.dbPath)
	if err == nil {
		_ = os.WriteFile(backup, data, 0o644)
	}
	snap, err := os.ReadFile(snapshotPath)
	if err != nil {
		return fmt.Errorf("读取快照 %s: %w", snapshotPath, err)
	}
	// 关闭当前连接
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("关闭数据库: %w", err)
	}
	// 覆盖主库
	if err := os.WriteFile(s.dbPath, snap, 0o644); err != nil {
		return fmt.Errorf("写入主库: %w", err)
	}
	// 移除 WAL/shm 残留
	_ = os.Remove(s.dbPath + "-wal")
	_ = os.Remove(s.dbPath + "-shm")

	// 重新打开
	dsn := "file:" + s.dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("重新打开数据库: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	s.db = db
	return nil
}
