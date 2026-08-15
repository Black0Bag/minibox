package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// mockMCPSource 模拟 MCP 服务器的 Go 源码（robust，支持并发）。
// 协议：JSON-RPC 2.0 over stdio newline-delimited。
const mockMCPSource = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type req struct {
	ID     int64  ` + "`json:\"id\"`" + `
	Method string ` + "`json:\"method\"`" + `
	Params json.RawMessage ` + "`json:\"params\"`" + `
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var r req
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		switch r.Method {
		case "initialize":
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "result": map[string]any{
				"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "mockserver", "version": "1.0"},
			}})
			fmt.Println(string(out))
		case "notifications/initialized":
			// 通知无响应
		case "tools/list":
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "result": map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "回显输入", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}}},
				{"name": "add", "description": "两数相加", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "number"}, "b": map[string]any{"type": "number"}}}},
			}}})
			fmt.Println(string(out))
		case "tools/call":
			var params struct {
				Name      string         ` + "`json:\"name\"`" + `
				Arguments map[string]any ` + "`json:\"arguments\"`" + `
			}
			if err := json.Unmarshal(r.Params, &params); err == nil {
				switch params.Name {
				case "echo":
					text, _ := params.Arguments["text"].(string)
					out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "回显: " + text}}}})
					fmt.Println(string(out))
					continue
				case "add":
					a := toFloat(params.Arguments["a"])
					b := toFloat(params.Arguments["b"])
					out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%g", a+b)}}}})
					fmt.Println(string(out))
					continue
				}
			}
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "error": map[string]any{"code": -32602, "message": "Unknown tool"}})
			fmt.Println(string(out))
		}
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	}
	return 0
}
`

// buildMockMCP 把模拟 MCP 服务器编译成可执行文件。
func buildMockMCP(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "mcp_server_main.go")
	if err := os.WriteFile(src, []byte(mockMCPSource), 0o600); err != nil {
		t.Fatalf("写 mock 源码失败: %v", err)
	}
	bin := filepath.Join(dir, "mcp-server")
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 mock MCP 服务器失败: %v\n%s", err, out)
	}
	return bin
}

// startMockMCPAdapter 启动基于 Go 二进制模拟服务器的适配器。
func startMockMCPAdapter(t *testing.T, name string) *MCPAdapter {
	t.Helper()
	dir := t.TempDir()
	bin := buildMockMCP(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adapter, err := NewMCPAdapter(ctx, name, bin, nil)
	if err != nil {
		t.Fatalf("NewMCPAdapter err=%v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter
}

// TestMCPAdapter_InitializeAndListTools 启动模拟 MCP 服务器，验证初始化 + 工具列表。
func TestMCPAdapter_InitializeAndListTools(t *testing.T) {
	adapter := startMockMCPAdapter(t, "mock")
	toolsList := adapter.Tools()
	if len(toolsList) != 2 {
		t.Fatalf("应获取 2 个工具，实际 %d", len(toolsList))
	}
	if toolsList[0].Name() != "mock__echo" {
		t.Fatalf("工具名=%q, 期望 mock__echo", toolsList[0].Name())
	}
	if toolsList[1].Name() != "mock__add" {
		t.Fatalf("工具名=%q, 期望 mock__add", toolsList[1].Name())
	}
}

// TestMCPAdapter_CallTool 调用远程工具。
func TestMCPAdapter_CallTool(t *testing.T) {
	adapter := startMockMCPAdapter(t, "mock")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
	adapter := startMockMCPAdapter(t, "mock")
	if !json.Valid(adapter.Tools()[0].JSONSchema()) {
		t.Fatal("JSONSchema 应合法")
	}
}

// TestMCPAdapter_ConcurrentCalls 并发调用远程工具（验证 sendAndRecv 串行化无数据竞争）。
func TestMCPAdapter_ConcurrentCalls(t *testing.T) {
	adapter := startMockMCPAdapter(t, "mock")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var echoTool tools.Tool
	for _, tl := range adapter.Tools() {
		if tl.Name() == "mock__echo" {
			echoTool = tl
			break
		}
	}
	if echoTool == nil {
		t.Fatal("未找到 mock__echo 工具")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := fmt.Sprintf("hello-%d", n)
			out, err := echoTool.Invoke(ctx, json.RawMessage(`{"text":"`+msg+`"}`))
			if err != nil {
				errs <- err
				return
			}
			if out != "回显: "+msg {
				errs <- fmt.Errorf("输出错配: got=%q want=%q", out, "回显: "+msg)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("并发调用错误: %v", err)
	}
}
