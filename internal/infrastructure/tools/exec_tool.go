package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// execTool 把 B8 已获取的二进制包装为可执行工具（B8 闭环）。
// Invoke 通过 IsolatedRun 执行（隔离 PATH + 独立参数传参，禁 shell）。
// 设计：LLM 经 acquire_tool 请求下载 → 下载成功 → 注册此工具 → 之后 LLM 可直接调用。
type execTool struct {
	name      string
	path      string
	spec      ToolSpec
	argSchema json.RawMessage
}

// NewExecTool 创建已获取工具的可执行包装。
// argSchema 为自定义参数 JSON Schema（缺省 {args:[string]} 列表形式）。
func NewExecTool(spec ToolSpec, path string, argSchema json.RawMessage) tools.Tool {
	schema := argSchema
	if len(schema) == 0 || string(schema) == "null" {
		schema = json.RawMessage(`{"type":"object","properties":{"args":{"type":"array","items":{"type":"string"},"description":"命令行参数列表"}},"required":["args"]}`)
	}
	return &execTool{name: spec.Name, path: path, spec: spec, argSchema: schema}
}

func (e *execTool) Name() string { return e.name }

func (e *execTool) Description() string {
	if e.spec.Description != "" {
		return e.spec.Description
	}
	return fmt.Sprintf("B8 获取的外部工具 %s（SHA-256 校验安装，隔离 PATH 执行）", e.name)
}

func (e *execTool) JSONSchema() json.RawMessage { return e.argSchema }

func (e *execTool) Metadata() tools.Metadata {
	return tools.Metadata{
		OpenWorld:        true,
		MaxResultSize:    1 << 20, // 1MB 输出上限
		RiskTier:         "high",
		RequiresApproval: true, // 外部二进制执行一律需批准（fail-closed）
	}
}

func (e *execTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Args    []string      `json:"args"`
		Timeout time.Duration `json:"timeout_ms"`
	}
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	// 允许按名（不含路径）或绝对路径引用二进制
	bin := e.path
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(filepath.Dir(e.path), bin)
	}
	return IsolatedRun(ctx, filepath.Dir(e.path), bin, params.Args, params.Timeout)
}

var _ tools.Tool = (*execTool)(nil)
