package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/memory"
)

// Server HTTP API 服务
type Server struct {
	cfg       *config.Config
	logger    *slog.Logger
	version   string
	startTime time.Time
	store     *memory.Store
}

// NewServer 创建 API 服务
func NewServer(cfg *config.Config, logger *slog.Logger, version string, store *memory.Store) *Server {
	return &Server{cfg: cfg, logger: logger, version: version, startTime: time.Now(), store: store}
}

// Router 构建路由（统一 /api/v1 前缀，N-13；RFC 7807 错误格式，N-09；限流，N-04）
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/json"))

	// CORS 预留（N-03）：当前 APP 不受限，为 PC Web 版 / API 文档在线查看预留
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         300,
	}))

	// 健康检查豁免限流（watchdog 依赖，N-04）
	r.Get("/api/v1/healthz", s.handleHealthz)

	// 其余 API 走限流（N-04：60/min，按 IP 键控）
	r.Group(func(r chi.Router) {
		r.Use(httprate.LimitBy(60, time.Minute, ipKey))
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/状态", s.handleStatus)
			r.Get("/版本", s.handleVersion)
			r.Get("/logs", s.handleLogs)
			r.Route("/供应商", func(r chi.Router) {
				r.Get("/", s.handleListProviders)
				r.Post("/", s.handleAddProvider)
				r.Get("/{name}", s.handleGetProvider)
				r.Put("/{name}", s.handleUpdateProvider)
				r.Delete("/{name}", s.handleDeleteProvider)
			})
			r.Route("/知识库", func(r chi.Router) {
				r.Post("/", s.handleKBCreate)
				r.Get("/", s.handleKBList)
				r.Get("/搜索", s.handleKBSearch)
				r.Delete("/", s.handleKBClear)
				r.Get("/{id}", s.handleKBGet)
				r.Put("/{id}", s.handleKBUpdate)
				r.Delete("/{id}", s.handleKBDelete)
			})
		})
	})

	return r
}

// ipKey 限流键：取对端地址。单后端内网个人助手场景无反向代理，RemoteAddr 即真实来源。
func ipKey(r *http.Request) (string, error) {
	return r.RemoteAddr, nil
}
