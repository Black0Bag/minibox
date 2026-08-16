// Package backup 提供备份实现（B18：VACUUM INTO 快照 + 回滚）。
// 设计：PRD B18，后端本地快照 + 前端拉取加密快照（前端只存不读）。
// 使用 SQLite VACUUM INTO（比 file copy 快 + 碎片整理，claude-hooks 2026 实证）。
package backup

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manager 备份管理器。
type Manager struct {
	dbPath    string
	backupDir string
}

// NewManager 创建备份管理器。
func NewManager(dbPath, backupDir string) *Manager {
	return &Manager{dbPath: dbPath, backupDir: backupDir}
}

// Snapshot 创建数据库快照（VACUUM INTO）。
func (m *Manager) Snapshot() (string, error) {
	if err := os.MkdirAll(m.backupDir, 0o750); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}

	tag := time.Now().UTC().Format("20060102_150405")
	snapPath := filepath.Join(m.backupDir, "minibox_"+tag+".db")

	// 打开源数据库
	src, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)", m.dbPath))
	if err != nil {
		return "", fmt.Errorf("打开源数据库失败: %w", err)
	}
	defer func() { _ = src.Close() }()

	// VACUUM INTO：原子备份 + 碎片整理
	_, err = src.Exec(fmt.Sprintf("VACUUM INTO '%s'", snapPath))
	if err != nil {
		return "", fmt.Errorf("VACUUM INTO 失败: %w", err)
	}

	// 收紧权限（N-05）
	if err := os.Chmod(snapPath, 0o600); err != nil {
		return snapPath, fmt.Errorf("设置备份权限失败: %w", err)
	}

	return snapPath, nil
}

// List 列出已有备份。
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".db" {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// Restore 从快照恢复（需先停服务，上层调用方保证）。
func (m *Manager) Restore(snapName string) error {
	snapPath := filepath.Join(m.backupDir, snapName)
	if _, err := os.Stat(snapPath); err != nil {
		return fmt.Errorf("快照不存在: %s", snapName)
	}
	// 原子替换：先删旧文件，再改名
	if err := os.Remove(m.dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除旧数据库失败: %w", err)
	}
	if err := os.Rename(snapPath, m.dbPath); err != nil {
		return fmt.Errorf("恢复快照失败: %w", err)
	}
	return nil
}
