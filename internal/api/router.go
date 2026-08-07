package api

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"github.com/Black0Bag/minibox/internal/agent"
	"github.com/Black0Bag/minibox/internal/compile"
	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/degrade"
	"github.com/Black0Bag/minibox/internal/device"
	"github.com/Black0Bag/minibox/internal/distill"
	"github.com/Black0Bag/minibox/internal/embed"
	"github.com/Black0Bag/minibox/internal/llm"
	"github.com/Black0Bag/minibox/internal/memory"
	"github.com/Black0Bag/minibox/internal/monitor"
	"github.com/Black0Bag/minibox/internal/subagent"
	"github.com/Black0Bag/minibox/internal/todolist"
	"github.com/Black0Bag/minibox/internal/tools"
)

// Server HTTP API 服务
type Server struct {
	cfg         *config.Config
	logger      *slog.Logger
	version     string
	startTime   time.Time
	store       *memory.Store
	embedClient *embed.Client
	compiler    *compile.Compiler
	distiller   *distill.Distiller
	chatLLM     *llm.Client
	agent       *agent.Agent
	subEngine   *subagent.Engine
	subSideDir  string
	plans       *todolist.Driver
	monitor     *monitor.Collector
	degrader    *degrade.Degrader
	devices     *device.Hub
}

// NewServer 创建 API 服务（配置了 embedding 时自动启用向量检索客户端）
func NewServer(cfg *config.Config, logger *slog.Logger, version string, store *memory.Store, compiler *compile.Compiler) *Server {
	s := &Server{cfg: cfg, logger: logger, version: version, startTime: time.Now(), store: store, compiler: compiler}
	if cfg.Embedding.Enabled && cfg.Embedding.BaseURL != "" {
		s.embedClient = embed.New(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model)
	}
	if store != nil {
		s.distiller = distill.NewDistiller(store)
	}
	for _, p := range cfg.Providers {
		if p.Enabled {
			s.chatLLM = llm.New(p.BaseURL, p.APIKey, p.Model)
			break
		}
	}
	// subagent 侧链日志目录 + to-do-list 计划目录（始终初始化，LLM 可选）
	s.subSideDir = filepath.Join(cfg.DataDir, "subagents")
	s.plans = todolist.NewDriver(filepath.Join(cfg.DataDir, "plans"))
	// 监控 + 降级 + 设备（M5）
	s.monitor = monitor.NewCollector()
	s.degrader = degrade.New(func() (float64, float64) {
		m, err := s.monitor.Collect()
		if err != nil {
			return 0, 0
		}
		return m.CPU.Percent, m.Memory.UsedPercent
	}, degrade.DefaultConfig())
	s.devices = device.NewHub(filepath.Join(cfg.DataDir, "devices"))
	if s.chatLLM != nil {
		reg := tools.NewRegistry()
		reg.Register(tools.TimeTool{})
		if store != nil {
			reg.Register(tools.NewKBSearchTool(store))
		}
		s.agent = agent.New(s.chatLLM, reg, 3)
		// subagent 引擎（M4）：Fan-out 并发（并发上限 3，深度先 2 层）
		runner := subagent.NewAgentRunner(s.chatLLM.Chat, reg, s.subSideDir)
		s.subEngine = subagent.New(runner, subagent.DefaultMaxConcurrent, subagent.DefaultMaxDepth)
	}
	return s
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
			r.Route("/快照", func(r chi.Router) {
				r.Post("/", s.handleSnapshotCreate)
				r.Get("/", s.handleSnapshotList)
				r.Post("/恢复", s.handleSnapshotRestore)
			})
			r.Route("/蒸馏", func(r chi.Router) {
				r.Post("/命中", s.handleDistillHit)
				r.Post("/反例", s.handleDistillNegative)
				r.Get("/", s.handleDistillList)
				r.Post("/老化", s.handleDistillDecay)
				r.Delete("/{keyword}", s.handleDistillDelete)
			})
			r.Route("/编译", func(r chi.Router) {
				r.Post("/", s.handleCompileSubmit)
				r.Get("/", s.handleCompileList)
				r.Get("/{id}", s.handleCompileGet)
			})
			r.Post("/对话/流式", s.handleChatStream)
			r.Post("/对话", s.handleChat)
			r.Route("/subagent", func(r chi.Router) {
				r.Post("/", s.handleSubagentDispatch)
				r.Get("/{id}/日志", s.handleSubagentLog)
			})
			r.Get("/监控", s.handleMonitor)
			r.Get("/降级", s.handleDegrade)
			r.Route("/设备", func(r chi.Router) {
				r.Post("/配对码", s.handleDevicePairingCode)
				r.Post("/", s.handleDeviceRegister)
				r.Get("/", s.handleDeviceList)
				r.Get("/{id}", s.handleDeviceGet)
				r.Post("/{id}/心跳", s.handleDeviceHeartbeat)
				r.Post("/{id}/离线", s.handleDeviceOffline)
				r.Delete("/{id}", s.handleDeviceUnregister)
				r.Get("/审计", s.handleDeviceAudit)
			})
			r.Route("/计划", func(r chi.Router) {
				r.Post("/", s.handlePlanCreate)
				r.Get("/", s.handlePlanList)
				r.Get("/{id}", s.handlePlanGet)
				r.Post("/{id}/执行", s.handlePlanExecute)
				r.Post("/{id}/回滚", s.handlePlanRollback)
				r.Delete("/{id}", s.handlePlanDelete)
			})
			r.Route("/知识库", func(r chi.Router) {
				r.Post("/", s.handleKBCreate)
				r.Get("/", s.handleKBList)
				r.Get("/搜索", s.handleKBSearch)
				r.Delete("/", s.handleKBClear)
				r.Post("/{id}/向量", s.handleKBSetVector)
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
