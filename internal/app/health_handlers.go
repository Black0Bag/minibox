package app

import (
	"net/http"
	"time"
)

// --- 健康检查+工具+权限域 ---

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
