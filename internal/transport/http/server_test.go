package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/transport"
)

func newTestServer() *Server {
	cfg := config.Default().Server
	logger := slog.New(slog.DiscardHandler)
	return New(cfg, logger, nil, "")
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
	var env transport.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("解析信封失败: %v", err)
	}
	if env.Type != "api.error" {
		t.Errorf("信封 type=%q, 期望 api.error", env.Type)
	}
}

// 业务端点测试已迁移至 internal/app/http_handlers_test.go