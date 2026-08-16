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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/Black0Bag/minibox/internal/transport"
)

// Server REST 传输层服务器。
// 组合根注入依赖，启动后监听 config.Server。
type Server struct {
	cfg     config.ServerConfig
	logger  *slog.Logger
	router  chi.Router
	httpSrv *http.Server
	// tools 工具注册表（/tools 端点用）
	toolReg *tools.Registry
}

// New 创建 REST 服务器。
// toolReg 可空（工具端点返回空列表）。
func New(cfg config.ServerConfig, logger *slog.Logger, toolReg *tools.Registry) *Server {
	s := &Server{
		cfg:     cfg,
		logger:  logger,
		toolReg: toolReg,
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

	// 健康检查（无需认证，对外）
	r.Get("/api/v1/health", s.handleHealth)
	r.Get("/api/v1/ready", s.handleReady)

	// 配置
	r.Route("/api/v1/config", func(r chi.Router) {
		r.Get("/", s.handleGetConfig)
	})

	// 工具
	r.Route("/api/v1/tools", func(r chi.Router) {
		r.Get("/", s.handleListTools)
	})

	// 服务器状态
	r.Get("/api/v1/server/status", s.handleServerStatus)

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

// respondEnvelope 根据 handler 结果封装成信封响应。
func (s *Server) respondEnvelope(w http.ResponseWriter, r *http.Request, typ string, data any) {
	env, err := transport.NewEnvelope("system", r.URL.Path, typ, data)
	if err != nil {
		http.Error(w, "信封封装失败", http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, env)
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

// 下方为各端点处理器
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.respondEnvelope(w, r, "api.health", map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.respondEnvelope(w, r, "api.ready", map[string]bool{"ready": true})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	s.respondEnvelope(w, r, "api.config.get", map[string]string{"note": "配置端点（Phase 6 骨架）"})
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	names := []string{}
	if s.toolReg != nil {
		names = s.toolReg.Names()
	}
	s.respondEnvelope(w, r, "api.tools.list", map[string]any{"tools": names})
}

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	s.respondEnvelope(w, r, "api.server.status", map[string]any{
		"uptime": time.Now().Unix(),
		"note":   "服务器状态（Phase 6 骨架）",
	})
}
