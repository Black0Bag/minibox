// Package app 是组合根（composition root）。
// 这是唯一能看到所有模块的地方：装配依赖、跨模块翻译。
// 模块之间绝不直接 import，全部通过这里调解。
package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/domain/permission"
	"github.com/Black0Bag/minibox/internal/domain/scheduler"
	"github.com/Black0Bag/minibox/internal/domain/setup"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/Black0Bag/minibox/internal/domain/worldbook"
	"github.com/Black0Bag/minibox/internal/infrastructure/backup"
	"github.com/Black0Bag/minibox/internal/infrastructure/engine"
	infrallm "github.com/Black0Bag/minibox/internal/infrastructure/llm"
	infrasched "github.com/Black0Bag/minibox/internal/infrastructure/scheduler"
	"github.com/Black0Bag/minibox/internal/infrastructure/storage"
	infratools "github.com/Black0Bag/minibox/internal/infrastructure/tools"
	"github.com/Black0Bag/minibox/internal/infrastructure/upgrade"
	"github.com/Black0Bag/minibox/internal/platform/degradation"
	"github.com/Black0Bag/minibox/internal/platform/fsutil"
	httptransport "github.com/Black0Bag/minibox/internal/transport/http"
	ssetransport "github.com/Black0Bag/minibox/internal/transport/sse"
	wstransport "github.com/Black0Bag/minibox/internal/transport/ws"
)

// App 是应用根对象，持有所有装配好的依赖。
type App struct {
	cfg    config.Config
	logger *slog.Logger
	db     *sql.DB
	http   *httptransport.Server
	sse    *ssetransport.Server
	ws     *wstransport.Server

	// 域依赖（阶段 1.1 端到端装配）
	llm       llm.Provider   // Router（多供应商）
	agent     *engine.Engine // Agent 引擎
	memory    memory.Store   // 知识库
	compiler  *storage.SQLiteCompiler
	distiller memory.Distiller
	assembler memory.Assembler
	embedder  *embedAdapter // 向量化客户端（query/passage 双模式）
	toolkit   *toolkit      // 工具执行器（带权限链）

	// Phase 7 装配
	wz       *setup.Wizard
	cred     *setup.DeviceCredential
	guard    *setup.PathGuard
	wbLoader *worldbook.Loader
	cron     *infrasched.CronScheduler
	logme    *fsutil.Logme        // 足迹系统（B21）
	monitor  *degradation.Monitor // 资源降级监控（B16）
	backup   *backup.Manager      // 备份管理（B18）
	upgrade  *upgrade.Manager     // 自升级（B19）

	// 会话（对话端点用）
	sessions *sessionHub
}

// New 创建 App，装配所有依赖。
func New(_ context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	a := &App{cfg: cfg, logger: logger}

	// 1. 数据库 + 知识库存储
	if err := a.openDatabase(cfg); err != nil {
		return nil, err
	}

	// 2. LLM 供应商 → Router
	router, err := buildLLMRouter(cfg, logger)
	if err != nil {
		_ = a.db.Close()
		return nil, err
	}
	a.llm = router

	// 蒸馏 LLM 提炼（B13：LLM 从内容提取结构化偏好）
	if d, ok := a.distiller.(*storage.SQLiteDistiller); ok {
		d.SetPrefExtractor(newPrefExtractor(router))
	}

	// 3. 工具注册表 + 内置工具 + 权限链 + 执行器
	if err := a.buildTools(cfg); err != nil {
		_ = a.db.Close()
		return nil, err
	}

	// 4. Agent 引擎 + 记忆门
	a.buildAgent()

	// 5. 传输层（三通道）
	a.http = httptransport.New(cfg.Server, logger, a.toolReg())
	a.sse = ssetransport.New(logger)
	a.ws = wstransport.New(logger)
	a.mountREST(a.http.Router()) // 阶段 1.2：挂全量 REST 业务端点

	// 阶段 1.3：三通道独立路由组（SSE 事件流 / WS 设备通道）
	a.http.Router().Mount("/api/v1/stream", a.sse.Handler())
	a.http.Router().Mount("/device/ws", a.ws.Handler())

	// 6. Phase 7 装配：首启向导 + 设备凭据 + 敏感路径 + 世界书 + 调度
	if err := a.buildSetup(cfg); err != nil {
		_ = a.db.Close()
		return nil, err
	}

	// 7. 会话 hub（对话端点）+ SSE 事件流推送
	a.sessions = newSessionHub(a.agent, a.compiler)
	a.sessions.SetPublisher(func(sessionID, typ string, data any) {
		_ = a.sse.Publish(sessionID, "agent", typ, data)
	})

	return a, nil
}

// openDatabase 打开 SQLite + 知识库存储 + 编译管道。
func (a *App) openDatabase(cfg config.Config) error {
	db, err := storage.Open(cfg.Database)
	if err != nil {
		return err
	}
	a.db = db

	tok, err := storage.NewJiebaTokenizer()
	if err != nil {
		_ = db.Close()
		return err
	}

	store := storage.NewSQLiteStore(db, tok)
	a.memory = store

	// 编译管道 + 蒸馏 + 组装器（阶段 2.1 接 embedding）
	a.compiler = storage.NewCompiler(db, store)
	if cfg.Embedding.BaseURL != "" && cfg.Embedding.Model != "" {
		dim := cfg.Embedding.Dimensions
		if dim <= 0 {
			dim = 1024 // 与 kb_vec 索引维度一致
		}
		ec := infrallm.NewEmbeddingClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Timeout, dim)
		ad := &embedAdapter{client: ec, model: cfg.Embedding.Model}
		a.embedder = ad
		a.compiler.SetEmbedder(ad)
	} else {
		a.logger.Warn("embedding 未配置，向量检索降级 FTS5/LIKE", "base_url", cfg.Embedding.BaseURL, "model", cfg.Embedding.Model)
	}
	a.distiller = storage.NewDistiller(db)
	a.assembler = storage.NewAssembler(store)
	return nil
}

// embedAdapter 把 EmbeddingClient 适配为 storage.Embedder。
type embedAdapter struct {
	client *infrallm.EmbeddingClient
	model  string
}

// EmbedBatch 实现 storage.Embedder（passage 模式，索引用）。
func (ad *embedAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return ad.client.EmbedBatch(ctx, ad.model, texts)
}

// EmbedQuery 生成查询向量（query 模式，检索用）。
func (ad *embedAdapter) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return ad.client.EmbedQuery(ctx, ad.model, text)
}

var _ storage.Embedder = (*embedAdapter)(nil)

// buildLLMRouter 从配置构建多供应商 Router。
func buildLLMRouter(cfg config.Config, logger *slog.Logger) (llm.Provider, error) {
	if len(cfg.LLM.Providers) == 0 {
		return nil, errors.New("LLM 配置缺 providers")
	}
	entries := make([]*infrallm.ProviderEntry, 0, len(cfg.LLM.Providers))
	for _, p := range cfg.LLM.Providers {
		timeout := p.Timeout
		if timeout <= 0 {
			timeout = cfg.LLM.Timeout // 供应商未配时用全局超时
		}
		if timeout <= 0 {
			timeout = 120 * time.Second // 兜底
		}
		client := infrallm.NewOpenAICompat(p.Name, p.BaseURL, p.APIKeys, timeout,
			infrallm.WithDefaultModel(cfg.LLM.DefaultModel))
		entries = append(entries, infrallm.NewProviderEntry(client, p.Name))
	}
	return infrallm.NewRouter(entries, infrallm.RouterConfig{}, logger), nil
}

// buildTools 装配工具注册表 + 内置工具 + 权限链 + 执行器。
func (a *App) buildTools(cfg config.Config) error {
	reg := tools.NewRegistry()

	// 内置文件工具（路径沙箱：root = 项目运行目录）
	validator := fsutil.NewPathValidator(cfg.Database.Path, cfg.Database.Path)
	for _, t := range []tools.Tool{
		infratools.NewReadFile(validator),
		infratools.NewWriteFile(validator),
		infratools.NewListDir(validator),
		infratools.NewSearchFiles(validator),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}

	// 知识库检索工具（RAG 3.0：Agent 自主决定何时检索）
	if a.memory != nil {
		if err := reg.Register(infratools.NewSearchKnowledge(a.memory, a.embedder)); err != nil {
			return err
		}
	}

	// 权限链（Phase 5 模块 30：禁止表 → 模式 → 元数据 → 批准 → 参数校验）
	chain := permission.NewChain(
		permission.NewForbiddenPolicy(),
		permission.NewModePolicy(),
		permission.NewMetadataPolicy(),
		permission.NewApprovalRequiredPolicy(),
		permission.NewArgsValidationPolicy(),
	)

	a.toolkit = &toolkit{reg: reg, policy: chain}
	return nil
}

// toolReg 返回工具注册表（供 /tools 端点）。
func (a *App) toolReg() *tools.Registry {
	if a.toolkit == nil {
		return tools.NewRegistry()
	}
	return a.toolkit.reg
}

// buildAgent 装配 Agent 引擎 + 强制记忆门。
func (a *App) buildAgent() {
	eng := engine.NewEngine(a.llm, a.toolkit, agent.Config{
		MaxSteps:      agent.MaxSteps,
		MaxTokens:     100000,
		Mode:          agent.ModePlan,
		RequirePlan:   true,
		ToolOutputCap: 8000,
	}, a.logger)

	// 强制记忆门（记忆中心化红线）
	if a.memory != nil {
		eng.SetMemoryGate(engine.NewMemoryGate(a.memory))
	}
	a.agent = eng
}

// buildSetup Phase 7 装配：向导 + 凭据 + 敏感路径 + 世界书 + 调度。
func (a *App) buildSetup(cfg config.Config) error {
	dataDir := filepath.Dir(cfg.Database.Path)
	a.wz = setup.New(setup.NewFileStore(filepath.Join(dataDir, "setup.json")))
	a.guard = setup.DefaultPathGuard()

	cred, err := setup.LoadOrCreate(filepath.Join(dataDir, "device.cred"))
	if err != nil {
		a.logger.Warn("设备凭据初始化失败", "err", err)
	}
	a.cred = cred

	a.wbLoader = worldbook.New(worldbook.Hooks{})
	a.cron = infrasched.New(&taskRunner{agent: a.agent, logger: a.logger}, a.logger)
	a.logme = fsutil.NewLogme(filepath.Join(dataDir, "logme"), 7*24*time.Hour)
	a.monitor = degradation.NewMonitor()
	a.backup = backup.NewManager(cfg.Database.Path, filepath.Join(dataDir, "backups"))
	a.upgrade = upgrade.NewManager(binaryPath())
	return nil
}

// Binary 返回当前二进制路径（自升级目标）。
func (a *App) Binary() string { return binaryPath() }

// bindPath 获取当前可执行文件路径。
func binaryPath() string {
	p, err := os.Executable()
	if err != nil {
		return "minibox"
	}
	return p
}

// taskRunner 调度任务执行器（接 Agent 引擎）。
type taskRunner struct {
	agent  *engine.Engine
	logger *slog.Logger
}

// Run 调度触发 → 启动一次 agent 运行（Plan 模式，禁写工具）。
func (r *taskRunner) Run(ctx context.Context, task scheduler.Task) (string, int, error) {
	if r.agent == nil {
		r.logger.Info("调度任务触发（无 agent 引擎）", "task", task.ID)
		return "", 0, nil
	}
	run, err := r.agent.Start(ctx, agent.Request{
		SessionID: "scheduled_" + task.ID,
		Message:   task.Prompt,
		Mode:      agent.ModePlan,
	})
	if err != nil {
		return "", 0, err
	}
	// 推进状态机直到 done/failed（受预算约束）
	for run.State == agent.StatePlanning || run.State == agent.StateActing {
		run, err = r.agent.Step(ctx, run.ID)
		if err != nil {
			return "", run.TokensSpent, err
		}
		if run.State == agent.StateAwaitingApproval {
			run, _ = r.agent.Approve(ctx, run.ID, false)
		}
	}
	return run.Answer, run.TokensSpent, nil
}

// Run 启动应用。
func (a *App) Run(ctx context.Context) error {
	if need, err := a.wz.NeedWizard(); err != nil {
		a.logger.Warn("向导状态读取失败", "err", err)
	} else if need {
		a.logger.Info("首次启动：等待完成配置向导", "data_dir", filepath.Dir(a.cfg.Database.Path))
	}

	a.logger.Info("minibox 启动",
		"port", a.cfg.Server.Port,
		"listen", a.cfg.Server.Listen,
		"llm_provider", a.cfg.LLM.DefaultProvider,
	)

	errCh := make(chan error, 1)
	go func() {
		if err := a.http.Start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return nil
	}
}

// Close 优雅关闭。
func (a *App) Close() error {
	a.logger.Info("minibox 关闭")
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout)
	defer cancel()
	if a.http != nil {
		if err := a.http.Shutdown(ctx); err != nil {
			return err
		}
	}
	if a.cron != nil {
		a.cron.Stop()
	}
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Config 返回应用配置。
func (a *App) Config() config.Config { return a.cfg }

// Logger 返回应用日志器。
func (a *App) Logger() *slog.Logger { return a.logger }

// ToolRegistry 返回工具注册表（供 /tools 端点）。
func (a *App) ToolRegistry() *tools.Registry { return a.toolReg() }

// LLM 返回 LLM 供应商（Router）。
func (a *App) LLM() llm.Provider { return a.llm }

// Agent 返回 Agent 引擎。
func (a *App) Agent() *engine.Engine { return a.agent }

// Memory 返回知识库。
func (a *App) Memory() memory.Store { return a.memory }

// Compiler 返回编译管道。
func (a *App) Compiler() memory.Compiler { return a.compiler }

// Distiller 返回蒸馏器。
func (a *App) Distiller() memory.Distiller { return a.distiller }

// Assembler 返回上下文组装器。
func (a *App) Assembler() memory.Assembler { return a.assembler }

// SSE 返回 SSE 传输服务器。
func (a *App) SSE() *ssetransport.Server { return a.sse }

// WS 返回 WebSocket 传输服务器。
func (a *App) WS() *wstransport.Server { return a.ws }

// Wizard 返回首次启动向导。
func (a *App) Wizard() *setup.Wizard { return a.wz }

// DeviceCredential 返回设备凭据。
func (a *App) DeviceCredential() *setup.DeviceCredential { return a.cred }

// PathGuard 返回敏感路径拦截器。
func (a *App) PathGuard() *setup.PathGuard { return a.guard }

// WorldbookLoader 返回世界书装载器。
func (a *App) WorldbookLoader() *worldbook.Loader { return a.wbLoader }

// Cron 返回调度中枢。
func (a *App) Cron() *infrasched.CronScheduler { return a.cron }

// Logme 返回足迹系统。
func (a *App) Logme() *fsutil.Logme { return a.logme }

// Monitor 返回降级监控。
func (a *App) Monitor() *degradation.Monitor { return a.monitor }

// Backup 返回备份管理器。
func (a *App) Backup() *backup.Manager { return a.backup }
