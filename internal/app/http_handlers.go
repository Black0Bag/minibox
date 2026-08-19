package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/config"
	plerrors "github.com/Black0Bag/minibox/internal/platform/errors"
	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/domain/permission"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	infratools "github.com/Black0Bag/minibox/internal/infrastructure/tools"
	"github.com/Black0Bag/minibox/internal/domain/scheduler"
	"github.com/Black0Bag/minibox/internal/infrastructure/upgrade"
	"github.com/Black0Bag/minibox/internal/transport"
)

// mountREST 把业务端点挂到组合根路由（阶段 1.2 全量 REST）。
func (a *App) mountREST(r chi.Router) {
	// 对话域
	r.Route("/api/v1/conversations", func(r chi.Router) {
		r.Post("/", a.handleConversationCreate)
		r.Get("/", a.handleConversationList)
		r.Get("/{id}", a.handleConversationGet)
		r.Post("/{id}/messages", a.handleConversationSend)
		r.Post("/{id}/rewind", a.handleConversationRewind)
	})

	// 知识库域
	r.Route("/api/v1/kb", func(r chi.Router) {
		r.Post("/search", a.handleKBSearch)
		r.Get("/store", a.handleKBList)
		r.Post("/store", a.handleKBCreate)
		r.Get("/store/{id}", a.handleKBGet)
		r.Patch("/store/{id}", a.handleKBUpdate)
		r.Delete("/store/{id}", a.handleKBDelete)
		r.Post("/compile", a.handleKBCompile)
		r.Get("/compile/{job_id}", a.handleKBCompileJob)
		r.Post("/distill", a.handleKBDistill)
		r.Get("/snapshots", a.handleKBSnapshotsList)
		r.Post("/snapshots", a.handleKBSnapshotCreate)
		r.Post("/rollback", a.handleKBRollback)
	})

	// LLM 域
	r.Route("/api/v1/llm", func(r chi.Router) {
		r.Get("/providers", a.handleLLMProviders)
		r.Get("/models", a.handleLLMModels)
		r.Post("/models/refresh", a.handleLLMModelsRefresh)
		r.Get("/feature-models", a.handleLLMFeatureModels)
		r.Patch("/feature-models", a.handleLLMUpdateFeatureModels)
	})

	// 调度域
	r.Route("/api/v1/schedules", func(r chi.Router) {
		r.Get("/", a.handleScheduleList)
		r.Post("/", a.handleScheduleCreate)
		r.Patch("/{id}", a.handleScheduleUpdate)
		r.Delete("/{id}", a.handleScheduleDelete)
		r.Post("/{id}/trigger", a.handleScheduleTrigger)
	})

	// 备份域
	r.Route("/api/v1/backups", func(r chi.Router) {
		r.Get("/", a.handleBackupList)
		r.Post("/export", a.handleBackupSnapshot)
	})

	// 自升级域
	r.Route("/api/v1/upgrade", func(r chi.Router) {
		r.Post("/check", a.handleUpgradeCheck)
		r.Post("/apply", a.handleUpgradeApply)
	})

	// 健康检查（Kubernetes 探针规范：liveness 轻量，readiness 含依赖检查）
	r.Get("/api/v1/health", a.handleHealth)
	r.Get("/api/v1/ready", a.handleReady)

	// 工具域
	r.Route("/api/v1/tools", func(r chi.Router) {
		r.Get("/", a.handleToolList)
		r.Post("/acquire", a.handleToolAcquire)
	})

	// 权限域
	r.Route("/api/v1/permissions", func(r chi.Router) {
		r.Get("/", a.handlePermissionsGet)
		r.Patch("/mode", a.handlePermissionsMode)
	})

	// 状态/配置
	r.Get("/api/v1/server/status", a.handleServerStatus)
	r.Get("/api/v1/config", a.handleConfig)
	r.Patch("/api/v1/config", a.handleConfigUpdate)
}

// respondJSON 写统一信封。
func respondJSON(w http.ResponseWriter, status int, env *transport.Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// respondOK 包装成功信封。
func (a *App) respondOK(w http.ResponseWriter, r *http.Request, typ string, data any) {
	env, err := transport.NewEnvelope("system", r.URL.Path, typ, data)
	if err != nil {
		http.Error(w, "信封封装失败", http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, env)
}

// respondErr 写 RFC 7807 错误信封（platform/errors 包）。
func (a *App) respondErr(w http.ResponseWriter, r *http.Request, status int, typ, detail string) {
	pd := plerrors.New(status, typ, "请求失败", detail).WithInstance(r.URL.Path)
	plerrors.Write(w, pd)
}

// --- 对话域 ---

// handleConversationCreate 创建会话。
func (a *App) handleConversationCreate(w http.ResponseWriter, r *http.Request) {
	s := a.sessions.Create()
	a.respondOK(w, r, "api.conversations.create", s)
}

// handleConversationList 列出会话。
func (a *App) handleConversationList(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.conversations.list", a.sessions.List())
}

// handleConversationGet 获取单个会话。
func (a *App) handleConversationGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, ok := a.sessions.Get(id)
	if !ok {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "会话不存在: "+id)
		return
	}
	a.respondOK(w, r, "api.conversations.get", s)
}

// conversationSendReq 发送消息请求体。
type conversationSendReq struct {
	Message string `json:"message"`
}

// handleConversationSend 发送用户消息，驱动 agent。
func (a *App) handleConversationSend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := a.sessions.Get(id); !ok {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "会话不存在: "+id)
		return
	}
	var req conversationSendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "message 必填")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	answer, err := a.sessions.Send(ctx, id, req.Message)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "agent_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.conversations.send", map[string]string{"answer": answer})
}

// conversationRewindReq 回退请求体。
type conversationRewindReq struct {
	Keep int `json:"keep"`
}

// handleConversationRewind 回退会话到指定消息数。
func (a *App) handleConversationRewind(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req conversationRewindReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := a.sessions.Rewind(id, req.Keep); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a.respondOK(w, r, "api.conversations.rewind", map[string]bool{"ok": true})
}

// --- 知识库域 ---

// kbSearchReq 检索请求。
type kbSearchReq struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

// handleKBSearch 检索知识库（三级降级）。
func (a *App) handleKBSearch(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "memory_unavailable", "知识库未就绪")
		return
	}
	var req kbSearchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "query 必填")
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	query := memory.SearchQuery{Text: req.Query, TopK: req.TopK, Tier: memory.TierStore}
	// 查询向量：embedder 可用时走 hybrid（vector+FTS5，asymmetric 模型须 query 模式）
	if a.embedder != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if vec, err := a.embedder.EmbedQuery(ctx, req.Query); err == nil && len(vec) > 0 {
			query.QueryVector = vec
		}
	}
	hits, err := a.memory.Search(r.Context(), query)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "search_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.search", map[string]any{"hits": hits})
}

// handleKBList 分页列出知识库。
func (a *App) handleKBList(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "memory_unavailable", "知识库未就绪")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	entries, err := a.memory.List(r.Context(), memory.TierStore, offset, limit)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "list_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.list", map[string]any{"entries": entries})
}

// handleKBGet 获取单条。
func (a *App) handleKBGet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	e, err := a.memory.Get(r.Context(), id, memory.TierStore)
	if err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "条目不存在")
		return
	}
	a.respondOK(w, r, "api.kb.store.get", e)
}

// handleKBCreate 写入条目。
func (a *App) handleKBCreate(w http.ResponseWriter, r *http.Request) {
	var e memory.Entry
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil || e.Content == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "content 必填")
		return
	}
	if err := a.memory.Upsert(r.Context(), e); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "upsert_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.create", map[string]bool{"ok": true})
}

// handleKBUpdate 更新条目。
func (a *App) handleKBUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var e memory.Entry
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil || id <= 0 {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	e.ID = id
	if err := a.memory.Upsert(r.Context(), e); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "upsert_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.update", map[string]bool{"ok": true})
}

// handleKBDelete 删除条目。
func (a *App) handleKBDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	if err := a.memory.Delete(r.Context(), id, memory.TierStore); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "delete_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.delete", map[string]bool{"ok": true})
}

// handleKBCompile 提交编译作业。
func (a *App) handleKBCompile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Source == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "source 必填")
		return
	}
	job, err := a.compiler.Compile(r.Context(), req.Source, memory.CompileOptions{})
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "compile_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.compile.create", job)
}

// handleKBCompileJob 查询编译作业状态。
func (a *App) handleKBCompileJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	job, err := a.compiler.GetJob(r.Context(), id)
	if err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "作业不存在: "+id)
		return
	}
	a.respondOK(w, r, "api.kb.compile.get", job)
}

// handleKBSnapshotsList 列出知识库快照。
func (a *App) handleKBSnapshotsList(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	list, err := a.backup.List()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "backup_list_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.snapshots.list", map[string]any{
		"count":     len(list),
		"snapshots": list,
	})
}

// handleKBSnapshotCreate 创建知识库快照（VACUUM INTO）。
func (a *App) handleKBSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	path, err := a.backup.Snapshot()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "snapshot_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.snapshots.create", map[string]string{
		"path":   path,
		"status": "ok",
	})
}

// handleKBRollback 从快照回滚知识库。
// 注意：回滚需重启服务使新数据库生效，当前返回 202 Accepted。
func (a *App) handleKBRollback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Snapshot string `json:"snapshot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}
	if req.Snapshot == "" {
		a.respondErr(w, r, http.StatusBadRequest, "missing_snapshot", "snapshot 字段必填")
		return
	}
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	if err := a.backup.Restore(req.Snapshot); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "rollback_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.rollback", map[string]string{
		"snapshot": req.Snapshot,
		"status":   "ok",
		"note":     "数据库已替换，建议重启服务使新数据库生效",
	})
}

// handleKBDistill 执行蒸馏。
func (a *App) handleKBDistill(w http.ResponseWriter, r *http.Request) {
	res, err := a.distiller.Distill(r.Context(), memory.DistillOptions{})
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "distill_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.distill", res)
}

// --- LLM 域 ---

// handleLLMProviders 列出 LLM 供应商。
func (a *App) handleLLMProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]string, 0, len(a.cfg.LLM.Providers))
	for _, p := range a.cfg.LLM.Providers {
		providers = append(providers, p.Name)
	}
	a.respondOK(w, r, "api.llm.providers", map[string]any{
		"default_provider": a.cfg.LLM.DefaultProvider,
		"providers":        providers,
	})
}

// handleLLMModels 列出模型（能力识别）。
func (a *App) handleLLMModels(w http.ResponseWriter, r *http.Request) {
	if a.llm == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "llm_unavailable", "LLM 未就绪")
		return
	}
	models, err := a.llm.Models(r.Context())
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "models_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.llm.models", map[string]any{"models": models})
}

// handleLLMModelsRefresh 刷新模型列表。
func (a *App) handleLLMModelsRefresh(w http.ResponseWriter, r *http.Request) {
	a.handleLLMModels(w, r)
}

// handleLLMFeatureModels 获取功能级模型配置（B6）。
func (a *App) handleLLMFeatureModels(w http.ResponseWriter, r *http.Request) {
	models := a.FeatureModels()
	if models == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "feature_models_unavailable", "FeatureRouter 未就绪")
		return
	}
	a.respondOK(w, r, "api.llm.feature_models", models)
}

// handleLLMUpdateFeatureModels 更新功能级模型配置（B6）。
func (a *App) handleLLMUpdateFeatureModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Configs []llm.FeatureConfig `json:"configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}

	models := &llm.FeatureModels{Configs: make(map[llm.Feature]llm.FeatureConfig)}
	for _, fc := range req.Configs {
		if err := fc.Validate(); err != nil {
			a.respondErr(w, r, http.StatusBadRequest, "invalid_feature_config", err.Error())
			return
		}
		models.Set(fc)
	}

	a.UpdateFeatureModels(models)
	a.respondOK(w, r, "api.llm.feature_models.updated", models)
}

// --- 调度域 ---

// handleScheduleList 列出调度任务。
func (a *App) handleScheduleList(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.schedules.list", a.cron.List())
}

// handleScheduleCreate 创建调度任务。
func (a *App) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	var task schedulerTask
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil || task.Name == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "name 必填")
		return
	}
	id, err := a.cron.Add(task.ToDomain())
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a.respondOK(w, r, "api.schedules.create", map[string]string{"id": id})
}

// handleScheduleUpdate 更新调度任务。
func (a *App) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.cron.Remove(id); err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var task schedulerTask
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	newID, err := a.cron.Add(task.ToDomain())
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a.respondOK(w, r, "api.schedules.update", map[string]string{"id": newID})
}

// handleScheduleDelete 删除调度任务。
func (a *App) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.cron.Remove(id); err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", err.Error())
		return
	}
	a.respondOK(w, r, "api.schedules.delete", map[string]bool{"ok": true})
}

// handleScheduleTrigger 立即触发。
func (a *App) handleScheduleTrigger(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.cron.RunNow(id); err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", err.Error())
		return
	}
	a.respondOK(w, r, "api.schedules.trigger", map[string]any{"id": id, "status": "triggered"})
}

// handleServerStatus 服务器状态。
func (a *App) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.server.status", map[string]any{
		"uptime": time.Since(a.startTime).Seconds(),
		"ok":     true,
	})
}

// handleConfigUpdate 更新配置（运行时热重载可更新项）。
func (a *App) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Logging  *config.LoggingConfig  `json:"logging,omitempty"`
		LLM      *struct {
			DefaultModel string `json:"default_model,omitempty"`
		} `json:"llm,omitempty"`
		Server   *struct {
			Port int `json:"port,omitempty"`
		} `json:"server,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}

	// 深拷贝当前配置，只更新允许的字段
	cfg := a.cfg
	if req.Logging != nil {
		if req.Logging.Level != "" {
			cfg.Logging.Level = req.Logging.Level
		}
		if req.Logging.Format != "" {
			cfg.Logging.Format = req.Logging.Format
		}
	}
	if req.LLM != nil && req.LLM.DefaultModel != "" {
		cfg.LLM.DefaultModel = req.LLM.DefaultModel
	}
	if req.Server != nil && req.Server.Port > 0 {
		cfg.Server.Port = req.Server.Port
	}

	a.UpdateConfig(cfg)
	a.respondOK(w, r, "api.config.updated", map[string]any{
		"server_port":      cfg.Server.Port,
		"default_model":    cfg.LLM.DefaultModel,
		"log_level":        cfg.Logging.Level,
	})
}

// handleConfig 配置摘要（隐藏密钥）。
func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.config.get", map[string]any{
		"server_port":      a.cfg.Server.Port,
		"listen":           a.cfg.Server.Listen,
		"db_path":          a.cfg.Database.Path,
		"default_provider": a.cfg.LLM.DefaultProvider,
		"default_model":    a.cfg.LLM.DefaultModel,
	})
}

// handleHealth 存活检查（liveness probe）。
// 纯进程级：只要服务在运行就返回 200，不依赖任何外部资源。
// 设计：Kubernetes 规范——liveness 不应检查依赖，否则误判重启。
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.health", map[string]any{
		"status":  "ok",
		"uptime":  time.Since(a.startTime).String(),
		"degrade": a.monitor.Level().String(),
	})
}

// handleReady 就绪检查（readiness probe）。
// 验证所有核心依赖是否就绪：DB 连接 / LLM 配置 / 向导完成。
// 设计：Kubernetes 规范——readiness 检查依赖，失败时停止流量。
func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]any{}

	// 数据库连接检查
	dbOK := false
	if a.db != nil {
		if err := a.db.PingContext(r.Context()); err == nil {
			dbOK = true
		}
	}
	checks["database"] = dbOK

	// LLM 配置检查
	llmOK := len(a.cfg.LLM.Providers) > 0
	checks["llm"] = llmOK

	// 首次启动向导检查（NeedWizard=false 表示已完成）
	wizardDone := true
	if a.wz != nil {
		need, err := a.wz.NeedWizard()
		if err != nil || need {
			wizardDone = false
		}
	}
	checks["wizard"] = wizardDone

	allOK := dbOK && llmOK && wizardDone
	if allOK {
		a.respondOK(w, r, "api.ready", map[string]any{
			"status": "ok",
			"checks": checks,
		})
	} else {
		a.respondErr(w, r, http.StatusServiceUnavailable, "not_ready", "服务未就绪")
	}
}

// handleToolList 列出所有已注册工具（含元数据 + JSON Schema）。
// 供前端展示工具清单、调试、审计。
func (a *App) handleToolList(w http.ResponseWriter, r *http.Request) {
	reg := a.toolReg()
	list := reg.List()
	type toolView struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Metadata    tools.Metadata    `json:"metadata"`
		JSONSchema  json.RawMessage   `json:"json_schema"`
	}
	views := make([]toolView, 0, len(list))
	for _, t := range list {
		views = append(views, toolView{
			Name:        t.Name(),
			Description: t.Description(),
			Metadata:    t.Metadata(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	a.respondOK(w, r, "api.tools.list", map[string]any{
		"count": len(views),
		"tools": views,
	})
}

// handlePermissionsGet 获取当前权限配置。
func (a *App) handlePermissionsGet(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.permissions.get", map[string]any{
		"mode": a.PermissionMode(),
		"modes": []permission.Mode{
			permission.ModeYolo,
			permission.ModeAcceptEdits,
			permission.ModeAsk,
			permission.ModePlan,
		},
	})
}

// handlePermissionsMode 更新权限模式（运行时动态切换）。
func (a *App) handlePermissionsMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode permission.Mode `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}
	switch req.Mode {
	case permission.ModeYolo, permission.ModeAcceptEdits, permission.ModeAsk, permission.ModePlan:
		a.SetPermissionMode(req.Mode)
		a.respondOK(w, r, "api.permissions.mode.updated", map[string]permission.Mode{
			"mode": req.Mode,
		})
	default:
		a.respondErr(w, r, http.StatusBadRequest, "invalid_mode", "无效权限模式: "+string(req.Mode))
	}
}

// handleToolAcquire 触发 B8 工具自动获取。
// 根据 spec 下载外部工具二进制（SHA-256 校验 + 隔离目录）。
func (a *App) handleToolAcquire(w http.ResponseWriter, r *http.Request) {
	var spec infratools.ToolSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}
	if spec.Name == "" || spec.URL == "" || spec.SHA256 == "" {
		a.respondErr(w, r, http.StatusBadRequest, "missing_fields", "name/url/sha256 必填")
		return
	}
	res, err := a.AcquireTool(r.Context(), spec)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "acquire_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.tools.acquire", map[string]any{
		"path":      res.Path,
		"installed": res.Installed,
	})
}

// --- 备份域 ---

// handleBackupList 列出备份。
func (a *App) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	list, err := a.backup.List()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "backup_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.backups.list", map[string]any{"backups": list})
}

// handleBackupSnapshot 创建快照。
func (a *App) handleBackupSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	path, err := a.backup.Snapshot()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "backup_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.backups.export", map[string]string{"path": path})
}

// --- 自升级域 ---

// upgradeCheckReq 检查更新请求体。
type upgradeCheckReq struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// handleUpgradeCheck 检查更新（校验 SHA-256 是否匹配，不实际下载）。
func (a *App) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if a.upgrade == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "upgrade_unavailable", "自升级未就绪")
		return
	}
	var req upgradeCheckReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "url 必填")
		return
	}
	a.respondOK(w, r, "api.upgrade.check", map[string]any{
		"upgrade_available": req.URL != "",
		"sha256_required":   req.SHA256 != "",
	})
}

// handleUpgradeApply 执行自升级。
func (a *App) handleUpgradeApply(w http.ResponseWriter, r *http.Request) {
	if a.upgrade == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "upgrade_unavailable", "自升级未就绪")
		return
	}
	var req upgradeCheckReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" || req.SHA256 == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "url 和 sha256 必填")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	replaced, err := a.upgrade.Do(ctx, upgrade.Spec{
		URL:    req.URL,
		SHA256: req.SHA256,
	})
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "upgrade_error", err.Error())
		return
	}
	if replaced {
		_ = a.logme.Log("upgrade.apply", "system", "升级成功，需重启生效")
	}
	a.respondOK(w, r, "api.upgrade.apply", map[string]bool{"replaced": replaced, "restart_required": replaced})
}

// schedulerTask 调度任务 HTTP DTO（与 domain/scheduler.Task 对齐）。
type schedulerTask struct {
	Name      string             `json:"name"`
	Type      scheduler.TaskType `json:"type"`
	Spec      string             `json:"spec"`
	Prompt    string             `json:"prompt"`
	Enabled   bool               `json:"enabled"`
	MaxWallMS int64              `json:"max_wall_ms"`
}

// ToDomain 转 domain 任务（默认 Type=schedule、Enabled=true）。
func (t schedulerTask) ToDomain() scheduler.Task {
	typ := t.Type
	if typ == "" {
		typ = scheduler.TypeSchedule
	}
	enabled := t.Enabled
	if t.Spec == "" && t.Type == "" {
		enabled = true
	}
	return scheduler.Task{
		Name:    t.Name,
		Type:    typ,
		Spec:    t.Spec,
		Prompt:  t.Prompt,
		Enabled: enabled,
		Budget: scheduler.Budget{
			MaxWall: time.Duration(t.MaxWallMS) * time.Millisecond,
		},
	}
}
