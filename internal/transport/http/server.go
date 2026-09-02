// Package http 提供 REST 传输层（chi v5 路由 + 统一信封）。
// 设计（进度跟踪 20260812 第2项 API 设计）：
//   - 三通道之一：REST（同步请求响应）
//   - /api/v1/* 前缀（B24），RFC 7807 错误
//   - 响应体包统一信封（B20），错误用 RFC 7807 + recovery_action
package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	plerrors "github.com/Black0Bag/minibox/internal/platform/errors"
)

// Server REST 传输层服务器。
// 组合根注入依赖，启动后监听 config.Server。
type Server struct {
	cfg    config.ServerConfig
	logger *slog.Logger
	router chi.Router
	// srvMu 保护 httpSrv：Start 在独立 goroutine 中赋值，
	// Shutdown 与测试可能从其他 goroutine 读取（CI race job 实证）。
	srvMu   sync.RWMutex
	httpSrv *http.Server
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

	// 认证中间件（Bearer Token）。
	// 白名单：健康检查（探针无凭据）与 WS 升级（/device/ws 有独立的
	// connect.auth + CredentialCheck 凭据机制）。
	// 实现在本包 auth.go，是全仓唯一的 REST 认证入口。
	r.Use(AuthMiddleware(s.authToken,
		"/api/v1/health",
		"/api/v1/ready",
		"/device/ws",
	))

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
	// ReadHeaderTimeout 防 Slowloris 慢速请求头攻击；旧配置文件未设置时回落 5s。
	readHeaderTimeout := s.cfg.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = 5 * time.Second
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadTimeout:       s.cfg.ReadTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}
	s.srvMu.Lock()
	s.httpSrv = srv
	s.srvMu.Unlock()

	s.logger.Info("REST 服务启动", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("REST 服务监听失败: %w", err)
	}
	return nil
}

// Shutdown 优雅关闭 HTTP 服务。未启动时直接返回 nil。
func (s *Server) Shutdown(ctx context.Context) error {
	srv := s.server()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// server 返回底层 http.Server（并发安全）；Start 未执行完时返回 nil。
func (s *Server) server() *http.Server {
	s.srvMu.RLock()
	defer s.srvMu.RUnlock()
	return s.httpSrv
}

// respondError 写 RFC 9457（原 7807）Problem Details 错误响应。
//
// 路由级错误（404/405）与业务 handler 的 app.respondErr、认证中间件的 401
// 使用同一格式：Content-Type: application/problem+json + 顶层
// type/title/status/detail/instance。前端只需一个错误解析器。
//
// 注意：错误响应不包装成功信封（Envelope）——把错误藏在 Envelope.data 里会让
// 客户端无法用统一路径判定失败（api.md「通用响应/错误」契约）。
// 成功信封由 app 包的 respondOK/respondJSON 负责；本包只处理路由级错误。
func respondError(w http.ResponseWriter, status int, typ, title, detail, instance string) {
	plerrors.Write(w, plerrors.ProblemDetail{
		Type:     typ,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	})
}

// 下方为各端点处理器（已迁移至 app/http_handlers.go，此处保留空）
// 健康检查 → app.handleHealth / app.handleReady
// 配置 → app.handleConfig / app.handleConfigUpdate
// 工具 → app.handleToolList / app.handleToolAcquire
// 状态 → app.handleServerStatus
