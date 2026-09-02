package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/config"
)

// authTestToken 测试用 token（长度与真实 64 位 hex token 一致，覆盖等长比较路径）。
const authTestToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newAuthTestHandler 构造「认证中间件 + 受保护端点」链路。
func newAuthTestHandler(token string, publicPaths ...string) http.Handler {
	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return AuthMiddleware(token, publicPaths...)(protected)
}

// TestAuthMiddleware_放行与拒绝 覆盖白名单、缺头、格式错误、错误 token、正确 token。
func TestAuthMiddleware_放行与拒绝(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{"白名单_health 免认证", "/api/v1/health", "", http.StatusOK},
		{"白名单_ready 免认证", "/api/v1/ready", "", http.StatusOK},
		{"白名单_device_ws 免认证", "/device/ws", "", http.StatusOK},
		{"缺 Authorization 头", "/api/v1/conversations/", "", http.StatusUnauthorized},
		{"非 Bearer scheme", "/api/v1/conversations/", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
		{"Bearer 后无凭据", "/api/v1/conversations/", "Bearer ", http.StatusUnauthorized},
		{"错误 token", "/api/v1/conversations/", "Bearer wrong-token", http.StatusUnauthorized},
		{"等长错误 token", "/api/v1/conversations/", "Bearer " + strings.Repeat("f", len(authTestToken)), http.StatusUnauthorized},
		{"正确 token", "/api/v1/conversations/", "Bearer " + authTestToken, http.StatusOK},
	}

	h := newAuthTestHandler(authTestToken, "/api/v1/health", "/api/v1/ready", "/device/ws")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("状态码=%d, 期望 %d（body=%s）", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// TestAuthMiddleware_scheme大小写不敏感 依据 RFC 9110 §11.1：auth-scheme 是
// case-insensitive token，Bearer/bearer/BEARER 都必须接受。
func TestAuthMiddleware_scheme大小写不敏感(t *testing.T) {
	h := newAuthTestHandler(authTestToken)
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		t.Run(scheme, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tools/", nil)
			req.Header.Set("Authorization", scheme+" "+authTestToken)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("scheme %q 状态码=%d, 期望 200", scheme, w.Code)
			}
		})
	}
}

// TestAuthMiddleware_401响应契约 回归：401 必须是 RFC 7807 problem+json，
// 且按 RFC 9110 §15.5.2 携带 WWW-Authenticate 挑战头。
// 修复前 401 返回 text/plain + 自造 {"error":...}，前端统一错误解析器无法处理。
func TestAuthMiddleware_401响应契约(t *testing.T) {
	cases := []struct {
		name          string
		authHeader    string
		wantErrorAttr bool // RFC 6750 §3：无凭据不带 error，凭据无效带 error="invalid_token"
	}{
		{"无凭据", "", false},
		{"凭据无效", "Bearer wrong", true},
	}

	h := newAuthTestHandler(authTestToken)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("状态码=%d, 期望 401", w.Code)
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Errorf("Content-Type=%q, 期望 application/problem+json 前缀", ct)
			}

			challenge := w.Header().Get("WWW-Authenticate")
			if challenge == "" {
				t.Error("401 必须携带 WWW-Authenticate 头（RFC 9110 §15.5.2）")
			}
			if !strings.Contains(challenge, `realm="minibox"`) {
				t.Errorf("WWW-Authenticate=%q, 期望含 realm=\"minibox\"", challenge)
			}
			hasError := strings.Contains(challenge, `error="invalid_token"`)
			if hasError != tc.wantErrorAttr {
				t.Errorf("WWW-Authenticate=%q, error 属性存在=%v, 期望 %v（RFC 6750 §3）",
					challenge, hasError, tc.wantErrorAttr)
			}

			var pd struct {
				Type     string `json:"type"`
				Title    string `json:"title"`
				Status   int    `json:"status"`
				Detail   string `json:"detail"`
				Instance string `json:"instance"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &pd); err != nil {
				t.Fatalf("401 响应体不是合法 JSON: %v（body=%s）", err, w.Body.String())
			}
			if pd.Type != "unauthorized" {
				t.Errorf("type=%q, 期望 unauthorized", pd.Type)
			}
			if pd.Status != http.StatusUnauthorized {
				t.Errorf("body.status=%d, 期望 401", pd.Status)
			}
			if pd.Title == "" || pd.Detail == "" {
				t.Errorf("title/detail 不得为空（title=%q detail=%q）", pd.Title, pd.Detail)
			}
			if pd.Instance != "/api/v1/config" {
				t.Errorf("instance=%q, 期望 /api/v1/config", pd.Instance)
			}
		})
	}
}

// TestAuthMiddleware_空token不启用认证 保持向后兼容：authToken 为空时全部放行
// （测试与本地无认证模式依赖该行为）。
func TestAuthMiddleware_空token不启用认证(t *testing.T) {
	h := newAuthTestHandler("")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("空 token 时状态码=%d, 期望 200（不启用认证）", w.Code)
	}
}

// TestServerAuthWiring 回归：Server 装配后 SSE 流路径受认证保护、健康检查放行。
// SSE (/api/v1/stream) 不在白名单内，Android 客户端必须带 Bearer 头
// （okhttp-sse 支持自定义请求头；浏览器原生 EventSource 不支持）。
//
// 注意：chi 在未注册任何路由时（mux.handler == nil）会直接走 NotFoundHandler
// 并跳过中间件链，因此这里必须先挂载路由，才能验证真实的中间件行为
// （生产路径由 app.mountREST/Mount 注册路由，中间件正常生效）。
func TestServerAuthWiring(t *testing.T) {
	srv := New(config.Default().Server, slog.New(slog.DiscardHandler), nil, authTestToken)

	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	srv.Router().Get("/api/v1/health", ok)
	srv.Router().Get("/api/v1/conversations/", ok)
	srv.Router().Mount("/api/v1/stream", http.HandlerFunc(ok))

	cases := []struct {
		path       string
		authHeader string
		wantStatus int
	}{
		{"/api/v1/health", "", http.StatusOK},                                // 白名单放行
		{"/api/v1/stream", "", http.StatusUnauthorized},                      // SSE 需认证
		{"/api/v1/stream", "Bearer " + authTestToken, http.StatusOK},         // 带头即通过
		{"/api/v1/conversations/", "", http.StatusUnauthorized},              // 普通端点需认证
		{"/api/v1/conversations/", "Bearer " + authTestToken, http.StatusOK}, // 带头即通过
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.authHeader != "" {
			req.Header.Set("Authorization", tc.authHeader)
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != tc.wantStatus {
			t.Errorf("%s (auth=%q) 状态码=%d, 期望 %d", tc.path, tc.authHeader, w.Code, tc.wantStatus)
		}
	}
}
