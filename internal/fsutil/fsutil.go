package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Black0Bag/minibox/internal/timestamp"
)

// 错误定义
var (
	ErrExists     = errors.New("文件已存在（防止覆盖删改，只允许新建）")
	ErrNotAllowed = errors.New("操作被防火墙拒绝")
)

// FS 统一文件操作入口：agent 的一切文件操作必经此 wrapper（代码级 enforce）
// 写操作自动追加 logme 留痕；读/遍历自动确保 logme 存在但不刷屏。
type FS struct {
	lm *LogMe
	ts *timestamp.Service
}

// NewFS 创建文件操作 wrapper
func NewFS(lm *LogMe, ts *timestamp.Service) *FS {
	return &FS{lm: lm, ts: ts}
}

// rel 返回相对路径（用于 logme 记录，避免记录绝对路径）
func rel(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return strings.TrimPrefix(abs, "/")
}

// EnsureDir 确保目录存在且 logme 就绪（进入文件夹时调用）
func (f *FS) EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s: %w", dir, err)
	}
	if f.lm != nil {
		return f.lm.Ensure()
	}
	return nil
}

// Read 读取文件（自动确保 logme；读操作不逐条追加记录避免刷屏）
func (f *FS) Read(path string) ([]byte, error) {
	if err := f.EnsureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件 %s: %w", path, err)
	}
	return data, nil
}

// ReadString 读取文件为字符串
func (f *FS) ReadString(path string) (string, error) {
	data, err := f.Read(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteNew 新建文件（已存在则拒绝——防火墙：禁止覆盖删改已有内容）
func (f *FS) WriteNew(path string, data []byte) error {
	if err := f.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: %s", ErrExists, path)
		}
		return fmt.Errorf("新建文件 %s: %w", path, err)
	}
	if _, werr := file.Write(data); werr != nil {
		file.Close()
		return fmt.Errorf("写入文件 %s: %w", path, werr)
	}
	if cerr := file.Close(); cerr != nil {
		return fmt.Errorf("关闭文件 %s: %w", path, cerr)
	}
	if f.lm != nil {
		_ = f.lm.Append("agent:minibox", fmt.Sprintf("新建文件 %s (%d B)", rel(path), len(data)))
	}
	return nil
}

// Append 追加写入（O_APPEND，天然只追加不覆盖）
func (f *FS) Append(path string, data []byte) error {
	if err := f.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开追加 %s: %w", path, err)
	}
	defer file.Close()
	if _, werr := file.Write(data); werr != nil {
		return fmt.Errorf("追加写入 %s: %w", path, werr)
	}
	if f.lm != nil {
		_ = f.lm.Append("agent:minibox", fmt.Sprintf("追加文件 %s (+%d B)", rel(path), len(data)))
	}
	return nil
}

// List 列出目录内容（自动确保 logme）
func (f *FS) List(dir string) ([]string, error) {
	if err := f.EnsureDir(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("遍历目录 %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Stat 文件信息
func (f *FS) Stat(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("获取文件信息 %s: %w", path, err)
	}
	return info, nil
}

// Exists 判断文件/目录是否存在
func (f *FS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
