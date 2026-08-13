// Package fsutil 提供文件操作的安全封装。
// 设计要点：
//   - 统一入口，后续 logme 足迹系统在此强制注入（只追加不删改）
//   - 路径沙箱检查（防止 agent 越权访问）
//   - 统一的错误包装（RFC 7807）
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathValidator 校验路径是否在允许范围内。
type PathValidator struct {
	// Root 允许访问的根目录。
	Root string
	// AllowedPaths 额外允许的路径。
	AllowedPaths []string
}

// NewPathValidator 创建路径校验器。
func NewPathValidator(root string, allowed ...string) *PathValidator {
	return &PathValidator{Root: root, AllowedPaths: allowed}
}

// Validate 校验路径是否允许访问。
// 规则：路径必须解析后仍在 root 内，或在额外允许列表中。
func (v *PathValidator) Validate(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("解析路径失败 %s: %w", path, err)
	}

	// 检查是否在 root 内
	rootAbs, err := filepath.Abs(v.Root)
	if err != nil {
		return fmt.Errorf("解析根目录失败 %s: %w", v.Root, err)
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return fmt.Errorf("路径不在允许范围内: %s", path)
	}
	if rel == "." || (!strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator))) {
		return nil
	}

	// 检查额外允许列表
	for _, allowed := range v.AllowedPaths {
		aAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		aRel, err := filepath.Rel(aAbs, abs)
		if err == nil && (aRel == "." || (!strings.HasPrefix(aRel, "..") && !strings.HasPrefix(aRel, string(filepath.Separator)))) {
			return nil
		}
	}

	return fmt.Errorf("路径不在允许范围内: %s", path)
}

// ReadFile 读取文件（带路径校验）。
func (v *PathValidator) ReadFile(path string) ([]byte, error) {
	if err := v.Validate(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}
	return data, nil
}

// WriteFile 写入文件（带路径校验）。
func (v *PathValidator) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := v.Validate(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("写入文件失败 %s: %w", path, err)
	}
	return nil
}

// Exists 检查路径是否存在。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
