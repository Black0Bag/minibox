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
)

// Server HTTP API 服务
type Server struct {
	cfg       *config.Config
	logger    *slog.Logger
	version   string
	startTime time.Time
}

// NewServer 创建 API 服务
func NewServer(cfg *config.Config, logger *slog.Logger, version string) *Server {
	return &Server{cfg: cfg, logger: logger, version: version, startTime: time.Now()}
}

// Router 构建路由（统一 /api/v1 前缀，N-13；RFC 7807 错误格式，N-09；限流，N-04）
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/json"))

	// CORS 预留（N-03）：当前 APP 不受限，为 PC Web 版 / API 文档在线查看预留
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		MaxAge:           300,
	}))

	// 健康检查豁免限流（watchdog 依赖，N-04）
	r.Get("/api/v1/healthz", s.handleHealthz)

	// 其余 API 走限流（N-04：60/min burst 10，按 IP）
	r.Group(func(r chi.Router) {
		r.Use(httprate.LimitByIP(60, time.Minute))
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
		})
	})

	return r
}
