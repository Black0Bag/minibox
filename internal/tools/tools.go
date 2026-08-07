package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Black0Bag/minibox/internal/memory"
)

// Tool 工具接口
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args string) (string, error)
}

// toolCallRe 文本协议工具调用：<tool name="工具名" args="参数"></tool>
// 供 agent 与 subagent 共用（单一解析来源，避免重复正则）
var toolCallRe = regexp.MustCompile(`<tool name="([^"]+)" args="([^"]*)"></tool>`)

// ParseToolCall 从输出中解析第一个工具调用，返回 (name, args, 是否命中)
func ParseToolCall(out string) (string, string, bool) {
	m := toolCallRe.FindStringSubmatch(out)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// CleanToolMarkers 清理输出中残留的工具标记
func CleanToolMarkers(out string) string {
	return strings.TrimSpace(toolCallRe.ReplaceAllString(out, ""))
}

// Registry 工具注册表（统一注册接口，支持运行时动态注册）
type Registry struct {
	tools map[string]Tool
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register 注册工具
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// List 列出工具
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// Run 执行工具
func (r *Registry) Run(ctx context.Context, name, args string) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("工具 %q 不存在", name)
	}
	return t.Run(ctx, args)
}

// Subset 按白名单过滤出工具子集（subagent 工具白名单，T-05）。
// 白名单为空表示不限制；不在白名单的工具被排除。
func (r *Registry) Subset(whitelist []string) *Registry {
	if len(whitelist) == 0 {
		sub := NewRegistry()
		for n, t := range r.tools {
			sub.tools[n] = t
		}
		return sub
	}
	allowed := make(map[string]bool, len(whitelist))
	for _, n := range whitelist {
		allowed[n] = true
	}
	sub := NewRegistry()
	for n, t := range r.tools {
		if allowed[n] {
			sub.tools[n] = t
		}
	}
	return sub
}

// LLMSpecs 生成供 LLM 使用的工具说明（文本协议）
func (r *Registry) LLMSpecs() string {
	var sb strings.Builder
	sb.WriteString("可用工具列表：\n")
	for _, t := range r.tools {
		fmt.Fprintf(&sb, "- %s：%s\n", t.Name(), t.Description())
	}
	sb.WriteString("调用格式：<tool name=\"工具名\" args=\"参数\"></tool>\n")
	return sb.String()
}

// ============ 内置工具 ============

// TimeTool 当前时间工具
type TimeTool struct{}

func (TimeTool) Name() string        { return "当前时间" }
func (TimeTool) Description() string { return "获取当前日期和时间" }
func (TimeTool) Run(ctx context.Context, args string) (string, error) {
	return time.Now().Format("2006-01-02 15:04:05"), nil
}

// KBSearchTool 知识库搜索工具
type KBSearchTool struct {
	store *memory.Store
}

// NewKBSearchTool 创建知识库搜索工具
func NewKBSearchTool(store *memory.Store) *KBSearchTool { return &KBSearchTool{store: store} }

func (t *KBSearchTool) Name() string { return "知识库搜索" }
func (t *KBSearchTool) Description() string {
	return "在知识库中搜索相关内容，参数为搜索关键词"
}
func (t *KBSearchTool) Run(ctx context.Context, args string) (string, error) {
	results, err := t.store.Search(strings.TrimSpace(args), "", 5)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "知识库无相关结果", nil
	}
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, r.Entry.Content)
	}
	return sb.String(), nil
}
