package app

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	plerrors "github.com/Black0Bag/minibox/internal/platform/errors"
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

	// 审批域
	r.Post("/api/v1/approvals/{run_id}", a.handleApprovalSubmit)

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

	// 性能监控域（Phase 3.5：CPU/内存/磁盘/进程指标）
	r.Route("/api/v1/monitor", func(r chi.Router) {
		r.Get("/metrics", a.handleMonitorMetrics)
		r.Get("/history", a.handleMonitorHistory)
	})

	// 团队协作域（T 系列：分诊 → 组建 → 讨论 → 结案）
	r.Route("/api/v1/teamwork", func(r chi.Router) {
		r.Get("/teams", a.handleTeamList)
		r.Post("/triage", a.handleTeamTriage)
		r.Post("/projects", a.handleTeamStart)
		r.Post("/projects/{id}/discuss", a.handleTeamDiscuss)
		r.Post("/projects/{id}/conclude", a.handleTeamConclude)
		r.Post("/staffing", a.handleTeamStaff)
	})
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
