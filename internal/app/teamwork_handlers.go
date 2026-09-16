package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/teamwork"
	"github.com/go-chi/chi/v5"
)

// --- 团队协作域+审批域 ---

// handleTeamList 列出全部常设团队。
func (a *App) handleTeamList(w http.ResponseWriter, r *http.Request) {
	if a.tw == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "teamwork_unavailable", "团队协作未就绪")
		return
	}
	a.respondOK(w, r, "api.teamwork.teams", map[string]any{"teams": a.tw.Scheduler().Catalog.All()})
}

// handleTeamTriage 前台分诊：根据需求推荐团队。
func (a *App) handleTeamTriage(w http.ResponseWriter, r *http.Request) {
	if a.tw == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "teamwork_unavailable", "团队协作未就绪")
		return
	}
	var req struct {
		Need string `json:"need"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Need == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "need 必填")
		return
	}
	triage, err := a.tw.Triage(r.Context(), req.Need)
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "triage_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.teamwork.triage", triage)
}

// handleTeamStart 确认团队后启动项目（组建团队 + 创建讨论）。
func (a *App) handleTeamStart(w http.ResponseWriter, r *http.Request) {
	if a.tw == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "teamwork_unavailable", "团队协作未就绪")
		return
	}
	var req struct {
		ProjectID string                    `json:"project_id"`
		Question  string                    `json:"question"`
		TeamID    string                    `json:"team_id"`
		Tasks     []teamwork.TaskDependency `json:"tasks,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Question == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "question 必填")
		return
	}
	p, err := a.tw.StartProject(r.Context(), req.ProjectID, req.Question, req.TeamID, req.Tasks)
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "start_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.teamwork.start", p)
}

// handleTeamDiscuss 驱动一轮成员讨论（LLM 生成提案）。
func (a *App) handleTeamDiscuss(w http.ResponseWriter, r *http.Request) {
	if a.tw == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "teamwork_unavailable", "团队协作未就绪")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Prompt string `json:"prompt"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	props, err := a.tw.RunDiscussion(r.Context(), id, req.Prompt)
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "discuss_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.teamwork.discuss", map[string]any{"proposals": props})
}

// handleTeamConclude 组长裁决结案。
func (a *App) handleTeamConclude(w http.ResponseWriter, r *http.Request) {
	if a.tw == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "teamwork_unavailable", "团队协作未就绪")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Verdict string `json:"verdict"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Verdict == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "verdict 必填")
		return
	}
	p, err := a.tw.Conclude(r.Context(), id, req.Verdict)
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "conclude_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.teamwork.conclude", p)
}

// handleApprovalSubmit 提交 Agent 工具调用审批（P0-2）。
func (a *App) handleApprovalSubmit(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	if runID == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "run_id 必填")
		return
	}
	if a.sessions == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "sessions_unavailable", "会话未就绪")
		return
	}

	var req struct {
		Approved bool `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := a.sessions.SubmitApproval(ctx, runID, req.Approved); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "approval_failed", err.Error())
		return
	}

	a.respondOK(w, r, "api.approvals.submit", map[string]any{
		"run_id":   runID,
		"approved": req.Approved,
	})
}

// handleTeamStaff 人力增援审批（自进化：人手不足上报）。
func (a *App) handleTeamStaff(w http.ResponseWriter, r *http.Request) {
	if a.tw == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "teamwork_unavailable", "团队协作未就绪")
		return
	}
	var req teamwork.StaffingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败: "+err.Error())
		return
	}
	decisions, alarm, err := a.tw.RequestStaffing(r.Context(), req)
	if err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "staffing_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.teamwork.staffing", map[string]any{"decisions": decisions, "alarm": alarm.String()})
}
