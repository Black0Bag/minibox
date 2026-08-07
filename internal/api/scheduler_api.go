package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/heartbeat"
	"github.com/Black0Bag/minibox/internal/scheduler"
	"github.com/Black0Bag/minibox/internal/skill"
)

// ============ 调度中枢 API（M6）============

// handleScheduleCreate 创建调度任务（POST /调度）
func (s *Server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type    string `json:"type"`
		Time    string `json:"time"`
		Date    string `json:"date"`
		Desc    string `json:"desc"`
		Payload string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	tk, err := s.sched.Create(scheduler.Task{Type: req.Type, Time: req.Time, Date: req.Date, Desc: req.Desc, Payload: req.Payload})
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", err.Error())
		return
	}
	s.logger.Info("创建调度任务", "id", tk.ID, "type", tk.Type)
	writeJSON(w, http.StatusCreated, tk)
}

// handleScheduleList 调度任务列表
func (s *Server) handleScheduleList(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.sched.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"任务": tasks, "总数": len(tasks)})
}

// handleScheduleGet 获取调度任务
func (s *Server) handleScheduleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tk, err := s.sched.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, tk)
}

// handleScheduleRun 立即执行到期任务（POST /调度/{id}/执行）——执行单个
func (s *Server) handleScheduleRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// 校验存在
	if _, err := s.sched.Get(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "任务不存在")
		return
	}
	// 直接标记完成 + 写知识库（由 kbSink 承接）
	_ = s.sched.MarkDone(id)
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "状态": "已执行"})
}

// handleScheduleCancel 取消任务
func (s *Server) handleScheduleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.sched.Cancel(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "状态": "已取消"})
}

// handleScheduleDelete 删除任务
func (s *Server) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.sched.Cancel(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "任务不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ============ 心跳任务 API（M6）============

// handleHeartbeatCreate 订阅心跳任务（POST /心跳）
func (s *Server) handleHeartbeatCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Desc       string `json:"desc"`
		Prompt     string `json:"prompt"`
		Boundaries string `json:"boundaries"`
		Interval   int    `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	tk, err := s.beats.Create(heartbeat.Task{Desc: req.Desc, Prompt: req.Prompt, Boundaries: req.Boundaries, Interval: req.Interval})
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tk)
}

// handleHeartbeatList 心跳任务列表
func (s *Server) handleHeartbeatList(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.beats.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"任务": tasks, "总数": len(tasks)})
}

// handleHeartbeatRun 立即执行心跳任务（POST /心跳/{id}/执行）
func (s *Server) handleHeartbeatRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.beats.Get(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "任务不存在")
		return
	}
	// 借助 RunDue 需要到期，这里直接调执行器
	if s.chatLLM == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "LLM 未配置", "")
		return
	}
	_ = s.beats.SetExecutorTmp(heartbeatExec{chat: s.chatLLM})
	_ = s.beats.ForceRun(id)
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "状态": "已执行"})
}

// handleHeartbeatUnsubscribe 退订
func (s *Server) handleHeartbeatUnsubscribe(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.beats.Unsubscribe(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "状态": "已退订"})
}

// ============ Skill API（M6）============

// handleSkillCreate 创建技能（POST /技能）
func (s *Server) handleSkillCreate(w http.ResponseWriter, r *http.Request) {
	var req skill.Skill
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	sk, err := s.skills.Create(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sk)
}

// handleSkillList 技能列表
func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	skills, err := s.skills.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"技能": skills, "总数": len(skills)})
}

// handleSkillMatch 匹配技能（GET /技能/匹配?q=）
func (s *Server) handleSkillMatch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "q 不能为空")
		return
	}
	sk, ok := s.skills.Match(q)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"命中": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"命中": true, "技能": sk})
}

// handleSkillRecord 沉淀成功工作流（POST /技能/沉淀）
func (s *Server) handleSkillRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string   `json:"name"`
		Desc  string   `json:"desc"`
		Steps []string `json:"steps"`
		Tags  []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	sk, err := s.skills.RecordSuccess(req.Name, req.Desc, req.Steps, req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", err.Error())
		return
	}
	s.logger.Info("沉淀技能", "name", sk.Name, "success", sk.SuccessCount)
	writeJSON(w, http.StatusCreated, sk)
}
