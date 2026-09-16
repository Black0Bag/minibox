package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/scheduler"
	"github.com/Black0Bag/minibox/internal/infrastructure/upgrade"
)

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
