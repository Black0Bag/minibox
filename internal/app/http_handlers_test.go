package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/permission"
	"github.com/Black0Bag/minibox/internal/domain/tools"
)

// mockApp 创建最小化 App 用于 HTTP 测试
func mockApp() *App {
	return &App{
		cfg:       config.Default(),
		startTime: mustParseTime("2026-08-17T12:00:00Z"),
		toolkit:   &toolkit{reg: tools.NewRegistry(), mode: permission.ModePlan},
	}
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestHandleHealth(t *testing.T) {
	a := mockApp()

	r := chi.NewRouter()
	r.Get("/api/v1/health", a.handleHealth)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/v1/health => %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data struct {
			Status string `json:"status"`
			Uptime string `json:"uptime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Data.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Data.Status, "ok")
	}
}

func TestHandleReady(t *testing.T) {
	a := mockApp()
	// 未配置 LLM → ready 应返回 503
	r := chi.NewRouter()
	r.Get("/api/v1/ready", a.handleReady)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /api/v1/ready (无 LLM) => %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleToolList(t *testing.T) {
	a := mockApp()
	// 注册一个测试工具
	reg := tools.NewRegistry()
	_ = reg.Register(&mockTool{name: "test_tool", desc: "A test tool"})
	a.toolkit.reg = reg

	r := chi.NewRouter()
	r.Get("/api/v1/tools", a.handleToolList)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/v1/tools => %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data struct {
			Count int `json:"count"`
			Tools []any `json:"tools"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Data.Count != 1 {
		t.Errorf("count = %d, want 1", resp.Data.Count)
	}
}

func TestHandlePermissionsGet(t *testing.T) {
	a := mockApp()
	a.toolkit.mode = permission.ModeAsk

	r := chi.NewRouter()
	r.Get("/api/v1/permissions", a.handlePermissionsGet)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/v1/permissions => %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data struct {
			Mode  permission.Mode   `json:"mode"`
			Modes []permission.Mode `json:"modes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Data.Mode != permission.ModeAsk {
		t.Errorf("mode = %q, want %q", resp.Data.Mode, permission.ModeAsk)
	}
}

func TestHandlePermissionsMode_Valid(t *testing.T) {
	a := mockApp()
	r := chi.NewRouter()
	r.Patch("/api/v1/permissions/mode", a.handlePermissionsMode)

	body := `{"mode":"yolo"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/permissions/mode", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PATCH /api/v1/permissions/mode (yolo) => %d, want %d", w.Code, http.StatusOK)
	}
	if a.PermissionMode() != permission.ModeYolo {
		t.Errorf("PermissionMode = %q, want %q", a.PermissionMode(), permission.ModeYolo)
	}
}

func TestHandlePermissionsMode_Invalid(t *testing.T) {
	a := mockApp()
	r := chi.NewRouter()
	r.Patch("/api/v1/permissions/mode", a.handlePermissionsMode)

	body := `{"mode":"invalid"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/permissions/mode", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("PATCH /api/v1/permissions/mode (invalid) => %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleConfigUpdate(t *testing.T) {
	a := mockApp()
	r := chi.NewRouter()
	r.Patch("/api/v1/config", a.handleConfigUpdate)

	body := `{"logging":{"level":"debug"},"llm":{"default_model":"test-model"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PATCH /api/v1/config => %d, want %d", w.Code, http.StatusOK)
	}
	if a.cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", a.cfg.Logging.Level, "debug")
	}
	if a.cfg.LLM.DefaultModel != "test-model" {
		t.Errorf("DefaultModel = %q, want %q", a.cfg.LLM.DefaultModel, "test-model")
	}
}

func TestHandleConfigUpdate_EmptyBody(t *testing.T) {
	a := mockApp()
	r := chi.NewRouter()
	r.Patch("/api/v1/config", a.handleConfigUpdate)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PATCH /api/v1/config (空) => %d, want %d", w.Code, http.StatusOK)
	}
}

// mockTool 实现 tools.Tool 接口用于测试
type mockTool struct {
	name string
	desc string
}

func (m *mockTool) Name() string                                      { return m.name }
func (m *mockTool) Description() string                               { return m.desc }
func (m *mockTool) JSONSchema() json.RawMessage                       { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) Metadata() tools.Metadata                          { return tools.Metadata{ReadOnly: true} }
func (m *mockTool) Invoke(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil }
