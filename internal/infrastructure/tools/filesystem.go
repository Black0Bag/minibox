// Package tools 内置工具实现（infrastructure 层）。
// 设计：
//   - 复用 platform/fsutil.PathValidator 做路径沙箱（防越权，golang-security）
//   - exec 一律独立参数传参，绝不拼 shell（禁 bash -c，golang-security）
//   - 工具只读/只写/破坏性元数据标注，供权限门控用
//   - 全部经 domain/tools.SafeInvoke 调用（panic 恢复 + 输出上限）
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/Black0Bag/minibox/internal/platform/fsutil"
)

// fileTool 基于 fsutil.PathValidator 的文件工具公共载体。
type fileTool struct {
	name    string
	desc    string
	schema  json.RawMessage
	meta    tools.Metadata
	fs      *fsutil.PathValidator
	fn      func(ctx context.Context, fs *fsutil.PathValidator, args map[string]json.RawMessage) (string, error)
}

func (f *fileTool) Name() string        { return f.name }
func (f *fileTool) Description() string { return f.desc }
func (f *fileTool) JSONSchema() json.RawMessage { return f.schema }
func (f *fileTool) Metadata() tools.Metadata    { return f.meta }

func (f *fileTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var args map[string]json.RawMessage
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	if args == nil {
		args = make(map[string]json.RawMessage)
	}
	return f.fn(ctx, f.fs, args)
}

var _ tools.Tool = (*fileTool)(nil)

// strArg 读取字符串参数。
func strArg(args map[string]json.RawMessage, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("缺少参数 %s", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("参数 %s 应为字符串: %w", key, err)
	}
	if s == "" {
		return "", fmt.Errorf("参数 %s 不能为空", key)
	}
	return s, nil
}

// NewReadFile 读取文件工具（只读，读后进上下文）。
func NewReadFile(fs *fsutil.PathValidator) tools.Tool {
	return &fileTool{
		name: "read_file",
		desc: "读取文本文件内容。输入 {path}。只读，安全。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{"path":{"type":"string","description":"绝对或相对路径"}},
			"required":["path"]
		}`),
		meta: tools.Metadata{
			ReadOnly:        true,
			ConcurrencySafe: true,
			SearchOrRead:    true,
			MaxResultSize:   1 << 16, // 64KB，防烧爆上下文
			RiskTier:        "low",
		},
		fs: fs,
		fn: func(_ context.Context, fs *fsutil.PathValidator, args map[string]json.RawMessage) (string, error) {
			p, err := strArg(args, "path")
			if err != nil {
				return "", err
			}
			data, err := fs.ReadFile(p)
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	}
}

// NewListDir 列目录工具（只读）。
func NewListDir(fs *fsutil.PathValidator) tools.Tool {
	return &fileTool{
		name: "list_dir",
		desc: "列出目录内容。输入 {path, recursive?}。只读。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"recursive":{"type":"boolean","default":false}
			},
			"required":["path"]
		}`),
		meta: tools.Metadata{
			ReadOnly:        true,
			ConcurrencySafe: true,
			SearchOrRead:    true,
			MaxResultSize:   1 << 16,
			RiskTier:        "low",
		},
		fs: fs,
		fn: func(_ context.Context, fs *fsutil.PathValidator, args map[string]json.RawMessage) (string, error) {
			p, err := strArg(args, "path")
			if err != nil {
				return "", err
			}
			if err := fs.Validate(p); err != nil {
				return "", err
			}
			recursive := false
			if raw, ok := args["recursive"]; ok {
				_ = json.Unmarshal(raw, &recursive)
			}
			entries, err := os.ReadDir(p)
			if err != nil {
				return "", fmt.Errorf("读取目录失败 %s: %w", p, err)
			}
			var sb strings.Builder
			for _, e := range entries {
				entry := e.Name()
				if e.IsDir() {
					entry += "/"
				}
				sb.WriteString(entry)
				sb.WriteString("\n")
				if recursive && e.IsDir() {
					sub := filepath.Join(p, e.Name())
					_ = walkDir(&sb, fs, sub, 1)
				}
			}
			return sb.String(), nil
		},
	}
}

// walkDir 递归目录（深度限制防环/爆栈）。
func walkDir(sb *strings.Builder, fs *fsutil.PathValidator, dir string, depth int) error {
	if depth > 6 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		entry := e.Name()
		if e.IsDir() {
			entry += "/"
			sb.WriteString(entry)
			sb.WriteString("\n")
			_ = walkDir(sb, fs, filepath.Join(dir, e.Name()), depth+1)
		} else {
			sb.WriteString(entry)
			sb.WriteString("\n")
		}
	}
	return nil
}

// NewSearchFiles 文件搜索工具（glob 模式，只读）。
func NewSearchFiles(fs *fsutil.PathValidator) tools.Tool {
	return &fileTool{
		name: "search_files",
		desc: "按 glob 模式搜索文件。输入 {pattern}。只读。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{"pattern":{"type":"string","description":"glob 模式如 **/*.go"}},
			"required":["pattern"]
		}`),
		meta: tools.Metadata{
			ReadOnly:        true,
			ConcurrencySafe: true,
			SearchOrRead:    true,
			MaxResultSize:   1 << 16,
			RiskTier:        "low",
		},
		fs: fs,
		fn: func(_ context.Context, fs *fsutil.PathValidator, args map[string]json.RawMessage) (string, error) {
			pat, err := strArg(args, "pattern")
			if err != nil {
				return "", err
			}
			// 解析 pattern 根（取第一个通配符前部分）校验沙箱
			root := patternRoot(pat)
			if root != "" {
				if err := fs.Validate(root); err != nil {
					return "", err
				}
			}
			matches, err := filepath.Glob(pat)
			if err != nil {
				return "", fmt.Errorf("搜索模式无效: %w", err)
			}
			var sb strings.Builder
			for _, m := range matches {
				// 二次校验：每个结果都在沙箱内
				if fs.Validate(m) != nil {
					continue
				}
				sb.WriteString(m)
				sb.WriteString("\n")
			}
			return sb.String(), nil
		},
	}
}

// patternRoot 提取 glob 模式中第一个通配符前的目录部分。
func patternRoot(pattern string) string {
	if i := strings.IndexAny(pattern, "*?["); i >= 0 {
		// 取到最后一个分隔符，保证是目录
		dir := pattern[:i]
		if j := strings.LastIndex(dir, string(filepath.Separator)); j >= 0 {
			return dir[:j]
		}
		return "."
	}
	return filepath.Dir(pattern)
}

// writeFileJSON write_file 参数。
type writeFileJSON struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

// NewWriteFile 写文件工具（破坏性，写需先 record_plan）。
func NewWriteFile(fs *fsutil.PathValidator) tools.Tool {
	return &fileTool{
		name: "write_file",
		desc: "写入/追加文件。输入 {path, content, append?}。破坏性。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"content":{"type":"string"},
				"append":{"type":"boolean","default":false}
			},
			"required":["path","content"]
		}`),
		meta: tools.Metadata{
			Destructive:      true,
			ConcurrencySafe:  false,
			MaxResultSize:    512,
			RiskTier:         "high",
			RequiresApproval: true,
		},
		fs: fs,
		fn: func(_ context.Context, fs *fsutil.PathValidator, args map[string]json.RawMessage) (string, error) {
			var in writeFileJSON
			raw, _ := json.Marshal(args)
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("参数解析失败: %w", err)
			}
			if in.Path == "" {
				return "", fmt.Errorf("缺少参数 path")
			}
			if err := fs.Validate(in.Path); err != nil {
				return "", err
			}
			if in.Append {
				if err := appendFile(fs, in.Path, []byte(in.Content)); err != nil {
					return "", err
				}
				return "已追加", nil
			}
			if err := fs.WriteFile(in.Path, []byte(in.Content), 0o644); err != nil {
				return "", err
			}
			return "已写入", nil
		},
	}
}

// appendFile 追加写（fsutil 无追加，这里补，仍走沙箱校验）。
func appendFile(fs *fsutil.PathValidator, path string, data []byte) error {
	if err := fs.Validate(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开追加目标失败 %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("追加写入失败 %s: %w", path, err)
	}
	return nil
}