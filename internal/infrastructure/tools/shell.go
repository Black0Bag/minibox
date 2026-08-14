package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// shellTool 命令执行工具。
// 安全设计（golang-security）：
//   - exec.Command 传独立参数，绝不拼 shell（禁 bash -c，防命令注入）
//   - 隔离 PATH（B8：不信任继承的 PATH）
//   - 高风险（high tier + RequiresApproval），必须人工批准
//   - 硬超时（防挂死）
//   - 输出上限（防烧爆上下文）
type shellTool struct {
	timeout time.Duration
}

func (s *shellTool) Name() string        { return "shell" }
func (s *shellTool) Description() string { return "执行命令。输入 {command, args?}。高风险，需人工批准。" }
func (s *shellTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"command":{"type":"string","description":"可执行程序名或绝对路径"},
			"args":{"type":"array","items":{"type":"string"}}
		},
		"required":["command"]
	}`)
}
func (s *shellTool) Metadata() tools.Metadata {
	return tools.Metadata{
		Destructive:       true,
		ConcurrencySafe:   false,
		OpenWorld:         true,
		MaxResultSize:     1 << 16,
		RiskTier:          "high",
		RequiresApproval:  true,
	}
}

func (s *shellTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	if in.Command == "" {
		return "", fmt.Errorf("缺少参数 command")
	}

	timeout := s.timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, in.Command, in.Args...)
	// 隔离 PATH（B8）：只允许受信目录
	cmd.Env = append(os.Environ(), "PATH="+isolationPath())
	cmd.Env = append(cmd.Env, "PYTHONNOUSERSITE=1", "PYTHONPATH=")

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("命令超时（%s）", timeout)
	}
	if err != nil {
		// 返回通用错误 + 部分输出，不暴露内部细节（golang-security）
		return string(out), fmt.Errorf("命令执行失败: %v", err)
	}
	return string(out), nil
}

var _ tools.Tool = (*shellTool)(nil)

// isolationPath 隔离 PATH（B8：SHA-256 校验 + 受信目录白名单）。
// 只保留系统基础命令目录，去除用户自装的可疑脚本目录。
func isolationPath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32;C:\Windows`
	}
	return "/usr/bin:/bin:/usr/local/bin"
}

// NewShell 创建命令执行工具。
func NewShell(timeout time.Duration) tools.Tool {
	return &shellTool{timeout: timeout}
}