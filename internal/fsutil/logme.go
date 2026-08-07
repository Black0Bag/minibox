package fsutil

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Black0Bag/minibox/internal/timestamp"
)

// 全局锁：所有 logme 写入共享一把锁，保证「读→diff→追加→序号分配」原子一致（T-09）
var logmeMu sync.Mutex

// LogMe 文件名
const LogMeFile = "logme"

// 敏感信息脱敏：API key 等 token 统一显示为 sk-***
var sensitiveRe = regexp.MustCompile(`(sk-)[A-Za-z0-9_\-]{8,}`)

// Sanitize 脱敏敏感信息（供所有记录/日志使用）
func Sanitize(s string) string {
	return sensitiveRe.ReplaceAllString(s, "sk-***")
}

// SeqStore 全局单调递增序号（持久化；M2 将迁移到数据库自增主键）
type SeqStore struct {
	mu   sync.Mutex
	path string
	next uint64
}

// NewSeqStore 加载或创建序号存储。path 为空则不持久化（仅内存）。
func NewSeqStore(path string) (*SeqStore, error) {
	s := &SeqStore{path: path, next: 1}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if n, perr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); perr == nil {
			s.next = n + 1
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取序号存储 %s: %w", path, err)
	}
	return s, nil
}

// Next 取下一个序号并持久化
func (s *SeqStore) Next() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.next
	s.next++
	if s.path != "" {
		if err := os.WriteFile(s.path, []byte(strconv.FormatUint(s.next-1, 10)), 0o600); err != nil {
			return 0, fmt.Errorf("持久化序号: %w", err)
		}
	}
	return n, nil
}

// LogMe 文件夹足迹管理器
type LogMe struct {
	dir  string
	seq  *SeqStore
	ts   *timestamp.Service
	file string
}

// NewLogMe 创建 LogMe 管理器
func NewLogMe(dir string, seq *SeqStore, ts *timestamp.Service) *LogMe {
	return &LogMe{dir: dir, seq: seq, ts: ts, file: filepath.Join(dir, LogMeFile)}
}

// semanticTemplate 语义区默认模板（agent 首次创建时使用）
const semanticTemplate = `════════════════════════════════════════════════════════════════
## LOGME · 文件夹语义区（首次创建后由 agent 填写，用户可编辑维护）
════════════════════════════════════════════════════════════════

### 用途
（本文件夹的用途说明，由 agent 首次进入时根据内容自动填写）

### 操作技巧
（本文件夹的操作经验，可在此沉淀）

### 注意事项
（本文件夹的注意事项）

---
## 操作记录区（agent 每次操作自动追加，只增不改）
---

`

// Ensure 确保 logme 存在；不存在则创建并写入语义区模板
func (l *LogMe) Ensure() error {
	logmeMu.Lock()
	defer logmeMu.Unlock()
	if _, err := os.Stat(l.file); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查 logme: %w", err)
	}
	// 创建 logme 并写入语义区模板
	dir := filepath.Dir(l.file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s: %w", dir, err)
	}
	f, err := os.OpenFile(l.file, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("创建 logme %s: %w", l.file, err)
	}
	if _, err := f.WriteString(semanticTemplate); err != nil {
		_ = f.Close()
		return fmt.Errorf("写入 logme 模板: %w", err)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("关闭 logme: %w", cerr)
	}
	slog.Debug("logme 已创建", "dir", l.dir)
	return nil
}

// Append 追加一条操作记录（只增不改；序号在锁内分配，保证原子一致）
// actor 形如 agent:minibox / agent:subagent-01 / user:UI
func (l *LogMe) Append(actor, action string) error {
	if err := l.Ensure(); err != nil {
		return err
	}
	action = Sanitize(action)
	logmeMu.Lock()
	defer logmeMu.Unlock()

	seq, err := l.seq.Next()
	if err != nil {
		return err
	}
	line := fmt.Sprintf("[%s] [#%06d] [%s] %s\n", l.ts.FormatNow(), seq, Sanitize(actor), action)
	f, err := os.OpenFile(l.file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 logme 追加: %w", err)
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("追加 logme: %w", err)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("关闭 logme: %w", cerr)
	}
	return nil
}

// Read 读取 logme 全文
func (l *LogMe) Read() (string, error) {
	logmeMu.Lock()
	defer logmeMu.Unlock()
	data, err := os.ReadFile(l.file)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取 logme: %w", err)
	}
	return string(data), nil
}

// ReadSemantic 读取语义区（操作区分隔符之前的内容）
func (l *LogMe) ReadSemantic() (string, error) {
	content, err := l.Read()
	if err != nil {
		return "", err
	}
	idx := strings.Index(content, "## 操作记录区")
	if idx < 0 {
		return content, nil
	}
	return content[:idx], nil
}

// ReadOperations 读取操作记录区全部记录行
func (l *LogMe) ReadOperations() ([]string, error) {
	content, err := l.Read()
	if err != nil {
		return nil, err
	}
	idx := strings.Index(content, "## 操作记录区")
	if idx < 0 {
		return nil, nil
	}
	var ops []string
	sc := bufio.NewScanner(strings.NewReader(content[idx:]))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.Contains(line, "[#") {
			ops = append(ops, line)
		}
	}
	return ops, sc.Err()
}
