// Package backup 提供备份实现（B18：VACUUM INTO 快照 + 回滚）。
// 设计：PRD B18，后端本地快照 + 前端拉取加密快照（前端只存不读）。
// 使用 SQLite VACUUM INTO（比 file copy 快 + 碎片整理，claude-hooks 2026 实证）。
package backup

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrInvalidSnapshotName 快照名非法（含路径分隔符、..、非 .db 等）。
// 属于调用方输入错误，上层应映射为 400 而非 500。
var ErrInvalidSnapshotName = errors.New("非法快照名")

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
// snapName 只接受 List() 返回的裸文件名；含路径分隔符或 .. 的输入一律拒绝，
// 防止 REST 层传入 "../../x.db" 之类路径穿越到备份目录之外替换任意文件。
func (m *Manager) Restore(snapName string) error {
	if err := validateSnapName(snapName); err != nil {
		return err
	}
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

// validateSnapName 校验快照名是裸文件名且为 .db，拒绝路径穿越。
// 所有失败都包装 ErrInvalidSnapshotName，便于上层用 errors.Is 映射 400。
func validateSnapName(snapName string) error {
	if snapName == "" {
		return fmt.Errorf("%w: 快照名为空", ErrInvalidSnapshotName)
	}
	// 拒绝任何路径成分：目录分隔符（含 Windows 反斜杠）、.. 与绝对路径
	if strings.ContainsAny(snapName, `/\`) ||
		snapName == ".." || strings.Contains(snapName, "..") ||
		filepath.Base(snapName) != snapName || filepath.IsAbs(snapName) {
		return fmt.Errorf("%w（不允许路径分隔符或 ..）: %s", ErrInvalidSnapshotName, snapName)
	}
	if filepath.Ext(snapName) != ".db" {
		return fmt.Errorf("%w（必须为 .db）: %s", ErrInvalidSnapshotName, snapName)
	}
	return nil
}
