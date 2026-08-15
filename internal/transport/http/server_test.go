package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/Black0Bag/minibox/internal/transport"
)

func newTestServer() *Server {
	cfg := config.Default().Server
	logger := slog.New(slog.DiscardHandler)
	return New(cfg, logger, nil)
}

// TestHealth 健康检查返回 200 + 信封。
func TestHealth(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("健康检查状态码=%d, 期望 200", w.Code)
	}
	var env transport.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("解析信封失败: %v", err)
	}
	if env.Type != "api.health" {
		t.Errorf("信封 type=%q, 期望 api.health", env.Type)
	}
	if env.Producer != "system" {
		t.Errorf("信封 producer=%q, 期望 system", env.Producer)
	}
	if env.SpecVersion != "1.0" {
		t.Errorf("信封 spec_version=%q, 期望 1.0", env.SpecVersion)
	}
	if err := env.Validate(); err != nil {
		t.Errorf("信封校验失败: %v", err)
	}
}

// TestReady 就绪检查。
func TestReady(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ready 状态码=%d, 期望 200", w.Code)
	}
	var env transport.Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	var data map[string]bool
	if err := env.UnmarshalData(&data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if !data["ready"] {
		t.Error("ready 应 true")
	}
}

// TestListTools 工具列表（无注册表时返回空数组）。
func TestListTools(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tools 状态码=%d, 期望 200", w.Code)
	}
	var env transport.Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	var data map[string]any
	if err := env.UnmarshalData(&data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	toolList, ok := data["tools"].([]any)
	if !ok {
		t.Fatalf("tools 字段类型错误: %T", data["tools"])
	}
	if len(toolList) != 0 {
		t.Errorf("空注册表应返回 0 个工具，实际 %d", len(toolList))
	}
}

// TestListTools_WithRegistry 有注册表时列出工具名。
func TestListTools_WithRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	mock := &mockTool{name: "read_file"}
	_ = reg.Register(mock)

	cfg := config.Default().Server
	logger := slog.New(slog.DiscardHandler)
	srv := New(cfg, logger, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var env transport.Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	var data map[string]any
	_ = env.UnmarshalData(&data)
	toolList := data["tools"].([]any)
	if len(toolList) != 1 || toolList[0] != "read_file" {
		t.Errorf("工具列表错误: %v", toolList)
	}
}

// TestUnknownRoute 未定义路由返回 404。
func TestUnknownRoute(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知路由状态码=%d, 期望 404", w.Code)
	}
}

// mockTool 测试用假工具。
type mockTool struct {
	name string
}

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string         { return m.name + " desc" }
func (m *mockTool) JSONSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) Metadata() tools.Metadata    { return tools.Metadata{} }
func (m *mockTool) Invoke(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

var _ tools.Tool = (*mockTool)(nil)
