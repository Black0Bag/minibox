package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeMCPTestServer 写一个模拟 MCP 服务器的 shell 脚本。
// 协议：JSON-RPC 2.0 over stdio，支持 initialize → tools/list → tools/call。
func writeMCPTestServer(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
# 模拟 MCP 服务器（最小实现）
while read line; do
  case "$line" in
    *\"initialize\"*)
      echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"mockserver","version":"1.0"}}}'
      ;;
    *\"notifications/initialized\"*)
      # 通知无响应
      ;;
    *\"tools/list\"*)
      echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"回显输入","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}},{"name":"add","description":"两数相加","inputSchema":{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}}]}}'
      ;;
    *\"tools/call\"*)
      # 解析工具名
      name=$(echo "$line" | sed 's/.*"name":"\([^"]*\)".*/\1/')
      case "$name" in
        echo)
          text=$(echo "$line" | sed 's/.*"text":"\([^"]*\)".*/\1/')
          echo '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"回显: '"$text"'"}]}}'
          ;;
        *)
          echo '{"jsonrpc":"2.0","id":3,"error":{"code":-32602,"message":"Unknown tool"}}'
          ;;
      esac
      ;;
  esac
done
`
	path := filepath.Join(dir, "mcp-server.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMCPAdapter_InitializeAndListTools 启动模拟 MCP 服务器，验证初始化 + 工具列表。
func TestMCPAdapter_InitializeAndListTools(t *testing.T) {
	dir := t.TempDir()
	serverPath := writeMCPTestServer(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter, err := NewMCPAdapter(ctx, "mock", "sh", []string{serverPath})
	if err != nil {
		t.Fatalf("NewMCPAdapter err=%v", err)
	}
	defer func() { _ = adapter.Close() }()

	tools := adapter.Tools()
	if len(tools) != 2 {
		t.Fatalf("应获取 2 个工具，实际 %d", len(tools))
	}

	// 验证工具名和命名规范（server__tool）
	if tools[0].Name() != "mock__echo" {
		t.Fatalf("工具名=%q, 期望 mock__echo", tools[0].Name())
	}
	if tools[1].Name() != "mock__add" {
		t.Fatalf("工具名=%q, 期望 mock__add", tools[1].Name())
	}
}

// TestMCPAdapter_CallTool 调用远程工具。
func TestMCPAdapter_CallTool(t *testing.T) {
	dir := t.TempDir()
	serverPath := writeMCPTestServer(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter, err := NewMCPAdapter(ctx, "mock", "sh", []string{serverPath})
	if err != nil {
		t.Fatalf("NewMCPAdapter err=%v", err)
	}
	defer func() { _ = adapter.Close() }()

	for _, tool := range adapter.Tools() {
		if tool.Name() == "mock__echo" {
			input := json.RawMessage(`{"text":"hello world"}`)
			out, err := tool.Invoke(ctx, input)
			if err != nil {
				t.Fatalf("echo Invoke err=%v", err)
			}
			if out != "回显: hello world" {
				t.Fatalf("echo out=%q", out)
			}
		}
	}
}

// TestMCPAdapter_NoServer 启动不存在的服务器应失败。
func TestMCPAdapter_NoServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := NewMCPAdapter(ctx, "bad", "/nonexistent/binary", nil)
	if err == nil {
		t.Fatal("不存在的服务器应返回错误")
	}
}

// TestMCPTool_JSONSchema 工具的 JSONSchema 返回正确。
func TestMCPTool_JSONSchema(t *testing.T) {
	dir := t.TempDir()
	serverPath := writeMCPTestServer(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter, err := NewMCPAdapter(ctx, "mock", "sh", []string{serverPath})
	if err != nil {
		t.Fatalf("NewMCPAdapter err=%v", err)
	}
	defer func() { _ = adapter.Close() }()

	v := json.Valid(adapter.Tools()[0].JSONSchema())
	if !v {
		t.Fatal("JSONSchema 应合法")
	}
}