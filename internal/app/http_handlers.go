package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/domain/scheduler"
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
	})

	// LLM 域
	r.Route("/api/v1/llm", func(r chi.Router) {
		r.Get("/providers", a.handleLLMProviders)
		r.Get("/models", a.handleLLMModels)
		r.Post("/models/refresh", a.handleLLMModelsRefresh)
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

	// 状态/配置
	r.Get("/api/v1/server/status", a.handleServerStatus)
	r.Get("/api/v1/config", a.handleConfig)
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

// respondErr 写 RFC 7807 错误信封。
func (a *App) respondErr(w http.ResponseWriter, r *http.Request, status int, typ, detail string) {
	env, _ := transport.NewEnvelope("system", r.URL.Path, "api.error", map[string]any{
		"type":     typ,
		"title":    "请求失败",
		"detail":   detail,
		"status":   status,
		"instance": r.URL.Path,
	})
	respondJSON(w, status, env)
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
	hits, err := a.memory.Search(r.Context(), memory.SearchQuery{Text: req.Query, TopK: req.TopK})
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
	tasks := a.cron.List()
	for _, t := range tasks {
		if t.ID == id {
			a.respondOK(w, r, "api.schedules.trigger", map[string]any{"id": id, "status": "accepted"})
			return
		}
	}
	a.respondErr(w, r, http.StatusNotFound, "not_found", "任务不存在: "+id)
}

// handleServerStatus 服务器状态。
func (a *App) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.server.status", map[string]any{
		"uptime": time.Now().Unix(),
		"ok":     true,
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
