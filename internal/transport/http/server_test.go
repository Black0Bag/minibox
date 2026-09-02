package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/config"
)

func newTestServer() *Server {
	cfg := config.Default().Server
	logger := slog.New(slog.DiscardHandler)
	return New(cfg, logger, nil, "")
}

// TestUnknownRoute 未定义路由返回 404 + RFC 9457 Problem Details。
// 回归：错误响应不得包装成成功 Envelope（前端需用统一错误解析器判定失败）。
func TestUnknownRoute(t *testing.T) {
	srv := newTestServer()
	// chi 在未注册任何路由时会跳过中间件链直接走 NotFoundHandler；
	// 这里注册一个无关路由，让 404 走完整的路由匹配路径。
	srv.Router().Get("/api/v1/existing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知路由状态码=%d, 期望 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type=%q, 期望 application/problem+json 前缀", ct)
	}

	var pd struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Detail   string `json:"detail"`
		Instance string `json:"instance"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pd); err != nil {
		t.Fatalf("解析 Problem Details 失败: %v（body=%s）", err, w.Body.String())
	}
	if pd.Type != "not_found" {
		t.Errorf("type=%q, 期望 not_found", pd.Type)
	}
	if pd.Status != http.StatusNotFound {
		t.Errorf("body.status=%d, 期望 404", pd.Status)
	}
	if pd.Instance != "/api/v1/nonexistent" {
		t.Errorf("instance=%q, 期望 /api/v1/nonexistent", pd.Instance)
	}
	if pd.Title == "" || pd.Detail == "" {
		t.Errorf("title/detail 不得为空（title=%q detail=%q）", pd.Title, pd.Detail)
	}
}

// TestMethodNotAllowed 已注册路径上使用不支持的方法返回 405 + Problem Details。
func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	srv.Router().Get("/api/v1/config", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码=%d, 期望 405", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type=%q, 期望 application/problem+json 前缀", ct)
	}
	var pd struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pd); err != nil {
		t.Fatalf("解析 Problem Details 失败: %v", err)
	}
	if pd.Type != "method_not_allowed" {
		t.Errorf("type=%q, 期望 method_not_allowed", pd.Type)
	}
	if pd.Status != http.StatusMethodNotAllowed {
		t.Errorf("body.status=%d, 期望 405", pd.Status)
	}
}

// 业务端点测试已迁移至 internal/app/http_handlers_test.go

// TestReadHeaderTimeoutConfigured 回归：http.Server 必须设置 ReadHeaderTimeout，
// 否则慢速请求头（Slowloris）可长期占用连接。
func TestReadHeaderTimeoutConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  func() config.ServerConfig
		want time.Duration
	}{
		{
			name: "默认配置",
			cfg:  func() config.ServerConfig { return config.Default().Server },
			want: 5 * time.Second,
		},
		{
			name: "显式配置生效",
			cfg: func() config.ServerConfig {
				c := config.Default().Server
				c.ReadHeaderTimeout = 3 * time.Second
				return c
			},
			want: 3 * time.Second,
		},
		{
			name: "旧配置文件缺省值回落 5s",
			cfg: func() config.ServerConfig {
				c := config.Default().Server
				c.ReadHeaderTimeout = 0
				return c
			},
			want: 5 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			// 端口 0：由内核分配空闲端口，避免测试间冲突
			cfg.Port = 0
			srv := New(cfg, slog.New(slog.DiscardHandler), nil, "")

			// Start 阻塞监听，用 goroutine 启动后立即读取已构造的 httpSrv
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = srv.Start()
			}()
			t.Cleanup(func() {
				_ = srv.Shutdown(context.Background())
				<-done
			})

			// 等待 httpSrv 构造完成
			deadline := time.Now().Add(2 * time.Second)
			for srv.httpSrv == nil && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if srv.httpSrv == nil {
				t.Fatal("httpSrv 未构造，Start 可能未执行")
			}
			if got := srv.httpSrv.ReadHeaderTimeout; got != tc.want {
				t.Errorf("ReadHeaderTimeout=%v, 期望 %v", got, tc.want)
			}
			if srv.httpSrv.ReadHeaderTimeout <= 0 {
				t.Error("ReadHeaderTimeout 必须为正值（Slowloris 防护）")
			}
		})
	}
}
