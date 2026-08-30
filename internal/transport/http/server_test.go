package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
