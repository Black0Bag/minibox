package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecTool_Invoke 用真实脚本验证 execTool 执行（隔离 PATH + 参数透传）。
func TestExecTool_Invoke(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "greet.sh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hello $1\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	tool := NewExecTool(ToolSpec{Name: "greet", Description: "greet tool"}, bin, nil)
	if tool.Name() != "greet" {
		t.Errorf("Name() = %q", tool.Name())
	}
	if !tool.Metadata().RequiresApproval {
		t.Error("外部二进制必须 RequiresApproval=true（fail-closed）")
	}

	out, err := tool.Invoke(context.Background(), []byte(`{"args":["world"]}`))
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("output = %q, want contains 'hello world'", out)
	}
}

// TestExecTool_InvalidInput JSON 解析失败应报错而非 panic。
func TestExecTool_InvalidInput(t *testing.T) {
	tool := NewExecTool(ToolSpec{Name: "x"}, "/nonexistent", nil)
	_, err := tool.Invoke(context.Background(), []byte(`{bad json`))
	if err == nil {
		t.Error("bad json should error")
	}
}

// TestExecTool_MissingBinary 二进制不存在应返回错误而非 panic。
func TestExecTool_MissingBinary(t *testing.T) {
	tool := NewExecTool(ToolSpec{Name: "x"}, "/definitely/not/here", nil)
	_, err := tool.Invoke(context.Background(), []byte(`{"args":[]}`))
	if err == nil {
		t.Error("missing binary should error")
	}
}
