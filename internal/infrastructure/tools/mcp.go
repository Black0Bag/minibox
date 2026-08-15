package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// 最小 MCP stdio 客户端（JSON-RPC 2.0 over stdin/stdout）。
// 不依赖 MCP Go SDK（单文件二进制红线，避免 segmentio/encoding 等重型依赖）。
// 协议：MCP 2025-06-18 规范（Initialize → ListTools → CallTool）。

// mcpRequest JSON-RPC 请求。
type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// mcpResponse JSON-RPC 响应。
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

// mcpError JSON-RPC 错误。
type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *mcpError) Error() string {
	return fmt.Sprintf("MCP 错误 %d: %s", e.Code, e.Message)
}

// mcpInitResult Initialize 结果。
type mcpInitResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// mcpToolDef 工具定义（tools/list 返回）。
type mcpToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// mcpListResult tools/list 结果。
type mcpListResult struct {
	Tools []mcpToolDef `json:"tools"`
}

// mcpCallParams tools/call 参数。
type mcpCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// mcpCallContent tools/call 结果内容项。
type mcpCallContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// mcpCallResult tools/call 结果。
type mcpCallResult struct {
	Content []mcpCallContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

// MCPAdapter 把 MCP 服务器子进程包装为工具注册表条目。
// 设计（edgecrab ADR-001 2026/ai-agents skill）：
//   - subprocess JSON-RPC 是首选（Go plugin 包是死路，依赖锁死）
//   - 命名 server__tool 防冲突（cronicle/Wox 实证）
//   - 子进程隔离，崩溃不影响主进程
type MCPAdapter struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	nextID  atomic.Int64
	tools   []tools.Tool
	closed  bool
}

// NewMCPAdapter 启动 MCP 服务器子进程，完成初始化握手，返回适配器。
// 子进程生命周期独立于调用方 ctx（用 context.WithoutCancel 派生，
// 避免 ctx 取消时误杀子进程——golang-context 实证）。
// ctx 仅用于初始化握手阶段的超时/取消控制。
func NewMCPAdapter(ctx context.Context, name, command string, args []string) (*MCPAdapter, error) {
	// 子进程上下文：脱离调用方取消链，由 Close() 管理生命周期
	procCtx := context.WithoutCancel(ctx)
	// #nosec G204 -- MCP 服务器命令由配置指定，非用户输入；stdin/stdout 走 JSON-RPC
	cmd := exec.CommandContext(procCtx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("MCP %s 创建 stdin 管道失败: %w", name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("MCP %s 创建 stdout 管道失败: %w", name, err)
	}
	cmd.Stderr = nil // 不拦截 stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("MCP %s 启动失败: %w", name, err)
	}

	a := &MCPAdapter{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
		nextID:  atomic.Int64{},
	}
	// MCP 工具结果可能超过 Scanner 默认 64KB 上限，扩大到 4MB（防长结果截断）
	a.scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	// 初始化握手
	if err := a.initialize(ctx); err != nil {
		_ = a.Close()
		return nil, err
	}

	// 获取工具列表
	if err := a.fetchTools(ctx); err != nil {
		_ = a.Close()
		return nil, err
	}

	return a, nil
}

// initialize 发送 initialize 请求，完成握手。
func (a *MCPAdapter) initialize(ctx context.Context) error {
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      a.nextID.Add(1),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "minibox",
				"version": "1.0.0",
			},
		},
	}
	var result mcpInitResult
	if err := a.sendAndRecv(ctx, req, &result); err != nil {
		return fmt.Errorf("initialize 失败: %w", err)
	}
	// 发送 initialized 通知（notifications/initialized）
	notif := mcpRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	return a.send(notif)
}

// fetchTools 获取服务器工具列表。
func (a *MCPAdapter) fetchTools(ctx context.Context) error {
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      a.nextID.Add(1),
		Method:  "tools/list",
	}
	var result mcpListResult
	if err := a.sendAndRecv(ctx, req, &result); err != nil {
		return fmt.Errorf("tools/list 失败: %w", err)
	}
	for _, t := range result.Tools {
		a.tools = append(a.tools, &mcpTool{
			adapter: a,
			server:  a.name,
			remote:  t.Name,
			desc:    t.Description,
			schema:  t.InputSchema,
		})
	}
	return nil
}

// Tools 返回该 MCP 服务器暴露的所有工具。
func (a *MCPAdapter) Tools() []tools.Tool {
	return a.tools
}

// Close 关闭 MCP 会话。
func (a *MCPAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	_ = a.stdin.Close()
	// 等待子进程退出
	done := make(chan struct{})
	go func() {
		_ = a.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = a.cmd.Process.Kill()
	}
	return nil
}

// send 发送 JSON-RPC 请求（不等待响应）。
func (a *MCPAdapter) send(req mcpRequest) error {
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = a.stdin.Write(line)
	return err
}

// sendAndRecv 发送 JSON-RPC 请求并等待响应。
// 并发安全：MCP stdio 是单管道请求/响应，整个读写用互斥锁串行化，
// 否则多 goroutine 共用 bufio.Scanner 会数据竞争 + 响应错配（golang-concurrency）。
func (a *MCPAdapter) sendAndRecv(ctx context.Context, req mcpRequest, result any) error {
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return fmt.Errorf("MCP 服务器 %s 已关闭", a.name)
	}
	if _, err := a.stdin.Write(line); err != nil {
		return err
	}

	// 读响应行（JSON-RPC 2.0 newline-delimited）
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !a.scanner.Scan() {
			if err := a.scanner.Err(); err != nil {
				return fmt.Errorf("MCP 读响应失败: %w", err)
			}
			return fmt.Errorf("MCP 连接意外关闭")
		}
		data := a.scanner.Bytes()
		var resp mcpResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		if resp.ID != req.ID {
			continue // 不是本请求的响应（可能是其他请求的或者通知），跳过
		}
		if resp.Error != nil {
			return resp.Error
		}
		return json.Unmarshal(resp.Result, result)
	}
}

// callTool 调用远程工具（Invoke 内部使用）。
func (a *MCPAdapter) callTool(ctx context.Context, remoteName string, args map[string]any) (string, error) {
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      a.nextID.Add(1),
		Method:  "tools/call",
		Params: mcpCallParams{
			Name:      remoteName,
			Arguments: args,
		},
	}
	var result mcpCallResult
	if err := a.sendAndRecv(ctx, req, &result); err != nil {
		return "", err
	}
	if result.IsError {
		errMsg := "MCP 工具执行错误"
		for _, c := range result.Content {
			if c.Text != "" {
				errMsg = c.Text
				break
			}
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	var texts []string
	for _, c := range result.Content {
		texts = append(texts, c.Text)
	}
	return strings.Join(texts, "\n"), nil
}

// mcpTool 包装一个远程 MCP 工具为 local domain/tools.Tool。
type mcpTool struct {
	adapter *MCPAdapter
	server  string
	remote  string
	desc    string
	schema  json.RawMessage
}

func (m *mcpTool) fullName() string { return m.server + "__" + m.remote }

func (m *mcpTool) Name() string                { return m.fullName() }
func (m *mcpTool) Description() string         { return m.desc }
func (m *mcpTool) JSONSchema() json.RawMessage { return m.schema }
func (m *mcpTool) Metadata() tools.Metadata {
	return tools.Metadata{
		OpenWorld:        true,
		MaxResultSize:    1 << 20,
		RiskTier:         "medium",
		RequiresApproval: false,
	}
}

func (m *mcpTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var args map[string]any
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	return m.adapter.callTool(ctx, m.remote, args)
}

var _ tools.Tool = (*mcpTool)(nil)
