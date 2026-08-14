package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestShell_Echo 简单命令（独立参数传参，无 shell）。
func TestShell_Echo(t *testing.T) {
	tool := NewShell(5 * time.Second)
	out, err := tool.Invoke(context.Background(), []byte(`{"command":"echo","args":["你好"]}`))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	if strings.TrimSpace(out) != "你好" {
		t.Fatalf("out=%q", out)
	}
}

// TestShell_MissingCommand 缺命令。
func TestShell_MissingCommand(t *testing.T) {
	tool := NewShell(5 * time.Second)
	if _, err := tool.Invoke(context.Background(), nil); err == nil {
		t.Fatal("缺 command 应失败")
	}
}

// TestShell_Timeout 超时。
func TestShell_Timeout(t *testing.T) {
	tool := NewShell(100 * time.Millisecond)
	_, err := tool.Invoke(context.Background(), []byte(`{"command":"sleep","args":["5"]}`))
	if err == nil {
		t.Fatal("超时应失败")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("err=%q, 应含'超时'", err.Error())
	}
}

// TestShell_RejectsShellInjection 命令注入防护（参数不被 shell 解释）。
func TestShell_RejectsShellInjection(t *testing.T) {
	tool := NewShell(5 * time.Second)
	// args 里的 ; 是字面量，不会执行注入命令
	out, err := tool.Invoke(context.Background(),
		[]byte(`{"command":"echo","args":["a; touch /tmp/pwned_by_injection"]}`))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	if strings.TrimSpace(out) != "a; touch /tmp/pwned_by_injection" {
		t.Fatalf("out=%q，说明参数被 shell 解析了", out)
	}
}