package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/llm"
	"github.com/Black0Bag/minibox/internal/subagent"
	"github.com/Black0Bag/minibox/internal/todolist"
)

// ============ subagent API（M4）============

// subagentDispatchRequest 派发请求：可单个或批量任务
type subagentDispatchRequest struct {
	Tasks []subagent.Task `json:"tasks"`
}

// resultView subagent 结果视图（错误文本化，便于 JSON 序列化）
type resultView struct {
	TaskID     string `json:"task_id"`
	Name       string `json:"name"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	LogFile    string `json:"log_file,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

func toResultView(r subagent.Result) resultView {
	return resultView{
		TaskID:     r.TaskID,
		Name:       r.Name,
		Output:     redact(r.Output),
		Error:      redact(r.ErrorString()),
		LogFile:    r.LogFile,
		DurationMs: r.Duration.Milliseconds(),
	}
}

// handleSubagentDispatch 派发 subagent 任务（Fan-out/Fan-in 同步执行）
func (s *Server) handleSubagentDispatch(w http.ResponseWriter, r *http.Request) {
	var req subagentDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if len(req.Tasks) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "tasks 不能为空")
		return
	}
	if s.subEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "subagent 未初始化", "未配置 LLM 供应商")
		return
	}
	// 任务 ID 缺省自动分配，避免重复
	for i := range req.Tasks {
		if strings.TrimSpace(req.Tasks[i].ID) == "" {
			req.Tasks[i].ID = fmt.Sprintf("sub-%d-%d", s.startTime.Unix(), i+1)
		}
	}
	results, err := s.subEngine.Run(r.Context(), req.Tasks)
	if err != nil {
		writeError(w, http.StatusBadGateway, "SUBMODEL_ERROR", "subagent 执行异常", redact(err.Error()))
		return
	}
	views := make([]resultView, 0, len(results))
	for _, res := range results {
		views = append(views, toResultView(res))
	}
	writeJSON(w, http.StatusOK, map[string]any{"结果": views, "总数": len(views)})
}

// handleSubagentLog 读取 subagent 侧链日志
func (s *Server) handleSubagentLog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" || strings.ContainsAny(id, `/\`) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "id 非法")
		return
	}
	path := filepath.Join(s.subSideDir, id+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "侧链日志不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"任务": id, "日志": redact(string(data))})
}

// ============ to-do-list 计划 API（M4）============

// planStepRequest 创建计划的步骤（仅需 desc）
type planStepRequest struct {
	Desc string `json:"desc"`
}

// planCreateRequest 创建计划请求
type planCreateRequest struct {
	Title string            `json:"title"`
	Goal  string            `json:"goal"`
	Steps []planStepRequest `json:"steps"`
}

// handlePlanCreate 创建长程计划（POST /计划）
func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	var req planCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if strings.TrimSpace(req.Goal) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "goal 不能为空")
		return
	}
	steps := make([]todolist.Step, 0, len(req.Steps))
	for _, st := range req.Steps {
		steps = append(steps, todolist.Step{Desc: st.Desc})
	}
	p, err := s.plans.Create(req.Title, req.Goal, steps)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", err.Error())
		return
	}
	s.logger.Info("创建计划", "id", p.ID, "steps", len(p.Steps))
	writeJSON(w, http.StatusCreated, p)
}

// handlePlanList 列出计划
func (s *Server) handlePlanList(w http.ResponseWriter, r *http.Request) {
	plans, err := s.plans.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"计划": plans, "总数": len(plans)})
}

// handlePlanGet 获取计划（中断续跑入口）
func (s *Server) handlePlanGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.plans.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "计划不存在")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePlanExecute 执行/续跑计划（POST /计划/{id}/执行）
func (s *Server) handlePlanExecute(w http.ResponseWriter, r *http.Request) {
	if s.chatLLM == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "LLM 未配置", "请先配置启用的供应商")
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := s.plans.Get(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "计划不存在")
		return
	}
	exec := s.planStepExecutor(r.Context())
	if err := s.plans.RunAll(r.Context(), id, exec); err != nil {
		writeError(w, http.StatusBadGateway, "PLAN_ERROR", "计划执行失败", redact(err.Error()))
		return
	}
	p, _ := s.plans.Get(id)
	writeJSON(w, http.StatusOK, p)
}

// planStepExecutor 构造单步执行函数：把计划步骤交给 LLM 执行
func (s *Server) planStepExecutor(ctx context.Context) todolist.StepFunc {
	return func(ctx context.Context, p *todolist.Plan, idx int) (string, error) {
		step := p.Steps[idx]
		messages := []llm.Message{
			{Role: "system", Content: "你是长程任务执行助手。正在执行一个多步骤计划，请只完成当前这一步，输出简洁可执行的结果。"},
			{Role: "user", Content: fmt.Sprintf("计划目标：%s\n当前步骤 %d/%d：%s", p.Goal, idx+1, len(p.Steps), step.Desc)},
		}
		return s.chatLLM.Chat(ctx, messages, llm.Options{MaxTokens: 2000, Temperature: 0.5})
	}
}

// handlePlanRollback 回滚到指定步骤（POST /计划/{id}/回滚 {index}）
func (s *Server) handlePlanRollback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if err := s.plans.Rollback(id, req.Index); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", err.Error())
		return
	}
	p, _ := s.plans.Get(id)
	writeJSON(w, http.StatusOK, p)
}

// handlePlanDelete 删除计划
func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.plans.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
