package app

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// --- 调度域+服务器状态 ---

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
// 原子语义：先解析请求体，再由 cron.Update 先建新任务、成功后才移除旧任务；
// 新任务非法时旧任务保持有效（修复此前"先 Remove 后 Add 失败即丢任务"的缺陷）。
func (a *App) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var task schedulerTask
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	newID, err := a.cron.Update(id, task.ToDomain())
	if err != nil {
		// 任务不存在 → 404；其余（cron 表达式非法、闹钟过期、未启用）→ 400
		if strings.Contains(err.Error(), "任务不存在") {
			a.respondErr(w, r, http.StatusNotFound, "not_found", err.Error())
			return
		}
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
