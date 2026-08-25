// Package http 提供 REST 传输层（chi v5 路由 + 统一信封）。
// 设计（进度跟踪 20260812 第2项 API 设计）：
//   - 三通道之一：REST（同步请求响应）
//   - /api/v1/* 前缀（B24），RFC 7807 错误
//   - 响应体包统一信封（B20），错误用 RFC 7807 + recovery_action
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/Black0Bag/minibox/internal/transport"
)

// Server REST 传输层服务器。
// 组合根注入依赖，启动后监听 config.Server。
type Server struct {
	cfg       config.ServerConfig
	logger    *slog.Logger
	router    chi.Router
	httpSrv   *http.Server
	// tools 工具注册表（/tools 端点用）
	toolReg   *tools.Registry
	authToken string // Bearer Token 认证
}

// New 创建 REST 服务器。
// toolReg 可空（工具端点返回空列表）。
// authToken 为 Bearer Token，空字符串表示不启用认证。
func New(cfg config.ServerConfig, logger *slog.Logger, toolReg *tools.Registry, authToken string) *Server {
	s := &Server{
		cfg:       cfg,
		logger:    logger,
		toolReg:   toolReg,
		authToken: authToken,
	}
	s.router = s.buildRouter()
	return s
}

// buildRouter 装配 chi 路由 + 中间件 + 端点。
func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	// 全局中间件
	r.Use(middleware.RequestID)   // 生成 X-Request-Id
	r.Use(middleware.Recoverer)   // panic 恢复 → 500
	r.Use(middleware.Compress(5)) // gzip 压缩
	// 注意：不用 middleware.RealIP（已废弃，存在 IP 欺骗漏洞 GHSA-3fxj-6jh8-hvhx，
	// golang-security 实证）；默认绑 127.0.0.1 单用户，无需透传客户端 IP。
	// 信封在 handler 层封装（respondEnvelope），不用全局缓冲中间件——
	// 缓冲会破坏流式/大响应（golang-code-style + SSE 红线：独立路由组）。

	// 认证中间件（若配置了 token）
	if s.authToken != "" {
		// 注意：这里需要引用 app 包的 AuthMiddleware，会产生循环依赖。
		// 解决方案：在 http 包内直接实现认证中间件，或使用函数注入。
		// 这里我们直接在 http 包内实现一个简单的认证中间件。
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// 白名单：健康检查和 WS 升级不需要 Bearer Token 认证
				// （WS 有自己的凭据校验机制：connect.auth + CredentialCheck）
				if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/ready" ||
					r.URL.Path == "/device/ws" {
					next.ServeHTTP(w, r)
					return
				}

				// 获取 Authorization 头
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
					return
				}

				// 必须是 Bearer 格式
				if !strings.HasPrefix(authHeader, "Bearer ") {
					http.Error(w, `{"error":"invalid authorization format, expected Bearer token"}`, http.StatusUnauthorized)
					return
				}

				// 提取并校验 token
				token := strings.TrimPrefix(authHeader, "Bearer ")
				if token == "" || token != s.authToken {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}

				next.ServeHTTP(w, r)
			})
		})
	}

	// 404/405 处理（统一信封 RFC 7807 错误）
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, http.StatusNotFound, "not_found", "资源不存在", "请求的路径未注册", r.URL.Path)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许", "该资源不支持此 HTTP 方法", r.URL.Path)
	})

	return r
}

// ServeHTTP 实现 http.Handler（供 http.Server 用）。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Router 返回 chi 路由（供组合根挂载业务端点）。
func (s *Server) Router() chi.Router {
	return s.router
}

// Start 启动 HTTP 监听（阻塞，支持优雅关闭）。
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Listen, s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  s.cfg.ReadTimeout,
		WriteTimeout: s.cfg.WriteTimeout,
		IdleTimeout:  s.cfg.IdleTimeout,
	}

	s.logger.Info("REST 服务启动", "addr", addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("REST 服务监听失败: %w", err)
	}
	return nil
}

// Shutdown 优雅关闭 HTTP 服务。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// respondJSON 写统一信封 JSON 响应。
func respondJSON(w http.ResponseWriter, status int, env *transport.Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}


// respondError 写 RFC 7807 错误信封（REST 错误路径专用）。
func respondError(w http.ResponseWriter, status int, typ, title, detail, instance string) {
	env, _ := transport.NewEnvelope("system", instance, "api.error", map[string]any{
		"type":     typ,
		"title":    title,
		"detail":   detail,
		"status":   status,
		"instance": instance,
	})
	respondJSON(w, status, env)
}

// 下方为各端点处理器（已迁移至 app/http_handlers.go，此处保留空）
// 健康检查 → app.handleHealth / app.handleReady
// 配置 → app.handleConfig / app.handleConfigUpdate
// 工具 → app.handleToolList / app.handleToolAcquire
// 状态 → app.handleServerStatus
