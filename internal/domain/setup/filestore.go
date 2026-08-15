package setup

// FileStore 向导状态的 JSON 文件存储（生产实现）。
// 权限 0600；文件不存在视为 pending；内容未知时 fail-closed 报错而非默许放行。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileStore 基于单个状态文件的 Store 实现。
type FileStore struct {
	path string
}

// NewFileStore 创建文件存储。
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Load 读取向导状态，文件不存在返回 StatusPending。
func (f *FileStore) Load() (Status, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return StatusPending, nil
	}
	if err != nil {
		return StatusPending, fmt.Errorf("读取向导状态失败: %w", err)
	}
	switch strings.TrimSpace(string(data)) {
	case "completed":
		return StatusCompleted, nil
	default:
		// fail-closed：内容未知时拒绝放行，要求人工修复
		return StatusPending, fmt.Errorf("向导状态文件内容未知: %s", f.path)
	}
}

// Save 持久化向导状态（0600）。
func (f *FileStore) Save(s Status) error {
	content := "pending"
	if s == StatusCompleted {
		content = "completed"
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}
	return os.WriteFile(f.path, []byte(content+"\n"), 0o600)
}
