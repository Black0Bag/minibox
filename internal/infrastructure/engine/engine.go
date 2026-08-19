// Package engine 提供 Agent 引擎的 infrastructure 实现。
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// Engine Agent 引擎实现（5 状态机）。
type Engine struct {
	llm    llm.Provider
	tools  agent.ToolExecutor
	cfg    agent.Config
	logger interface {
		Info(msg string, args ...any)
		Warn(msg string, args ...any)
		Error(msg string, args ...any)
	}
	gate *MemoryGate // 强制记忆门（记忆中心化）
	store RunStorer  // 运行持久化（B5 崩溃续跑，nil=纯内存模式）

	mu   sync.RWMutex
	runs map[string]*agent.Run
}

// RunStorer 运行持久化接口（B5 崩溃续跑）。
// nil 表示纯内存模式（测试用），非 nil 则每次状态变更后落盘。
type RunStorer interface {
	SaveRun(run *agent.Run) error
	LoadRun(runID string) (*agent.Run, error)
	ListPendingRuns() ([]*agent.Run, error)
	DeleteRun(runID string) error
}

// NewEngine 创建 Agent 引擎。
func NewEngine(llmProvider llm.Provider, tools agent.ToolExecutor, cfg agent.Config, logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}) *Engine {
	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = agent.MaxSteps
	}
	if cfg.ToolOutputCap == 0 {
		cfg.ToolOutputCap = 8000
	}
	return &Engine{
		llm:    llmProvider,
		tools:  tools,
		cfg:    cfg,
		logger: logger,
		runs:   make(map[string]*agent.Run),
	}
}

// SetMemoryGate 设置记忆门（记忆中心化，composition root 注入）。
func (e *Engine) SetMemoryGate(g *MemoryGate) {
	e.gate = g
}

// SetStore 设置运行持久化存储（B5 崩溃续跑）。
func (e *Engine) SetStore(s RunStorer) {
	e.store = s
}

// persistRun 持久化 Run 到 DB（store 为 nil 时跳过，不阻断流程）。
func (e *Engine) persistRun(run *agent.Run) {
	if e.store == nil {
		return
	}
	if err := e.store.SaveRun(run); err != nil {
		if e.logger != nil {
			e.logger.Error("运行持久化失败", "run_id", run.ID, "err", err)
		}
	}
}

// Start 启动一次运行。
func (e *Engine) Start(_ context.Context, req agent.Request) (*agent.Run, error) {
	run := &agent.Run{
		ID:        newRunID(),
		SessionID: req.SessionID,
		State:     agent.StatePlanning,
		Mode:      req.Mode,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if run.Mode == "" {
		run.Mode = e.cfg.Mode
	}

	// 历史消息
	run.Messages = append(run.Messages, req.History...)
	run.Messages = append(run.Messages, llm.Message{Role: llm.RoleUser, Content: req.Message})

	e.mu.Lock()
	e.runs[run.ID] = run
	e.mu.Unlock()
	e.persistRun(run)

	return run, nil
}

// Step 状态机单步推进。
// 依据 multigrid 2026：step() 返回下一个状态，driver 每次转换后持久化。
func (e *Engine) Step(ctx context.Context, runID string) (*agent.Run, error) {
	e.mu.Lock()
	run, ok := e.runs[runID]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("运行不存在: %s", runID)
	}

	// 预算检查（在每个转换开头，multigrid 实证：无法被绕过）
	if run.Steps >= e.cfg.MaxSteps {
		run.State = agent.StateFailed
		run.Error = "步数预算耗尽"
		run.UpdatedAt = time.Now()
		e.persistRun(run)
		return run, nil
	}

	switch run.State {
	case agent.StatePlanning:
		return e.stepPlanning(ctx, run)

	case agent.StateActing:
		return e.stepActing(ctx, run)

	case agent.StateAwaitingApproval, agent.StateAwaitingInput, agent.StateDone, agent.StateFailed:
		// 惰性状态，不做转换
		return run, nil
	}

	return run, nil
}

// stepPlanning 规划状态：调用 LLM 决定下一步。
func (e *Engine) stepPlanning(ctx context.Context, run *agent.Run) (*agent.Run, error) {
	// 强制记忆门：LLM 调用前检索记忆并注入（记忆中心化）
	messages := run.Messages
	if e.gate != nil {
		injected, _, err := e.gate.Inject(ctx, llm.Request{Messages: run.Messages})
		if err == nil {
			messages = injected
		}
	}

	resp, err := e.llm.Complete(ctx, llm.Request{
		Feature:  llm.FeatureAgent, // B6 对话/Agent 引擎使用独立模型
		Model:    "",               // 用默认模型（FeatureRouter 按配置替换）
		Messages: messages,
		Tools:    e.toolsSchema(),
	})
	if err != nil {
		run.State = agent.StateFailed
		run.Error = err.Error()
		run.UpdatedAt = time.Now()
		e.persistRun(run)
		return run, nil
	}

	if e.logger != nil {
		e.logger.Info("LLM 响应",
			"run_id", run.ID, "steps", run.Steps,
			"content_len", len(resp.Content),
			"tool_calls", len(resp.ToolCalls),
			"finish", resp.FinishReason,
			"msg_count", len(messages),
			"content_preview", truncate(resp.Content, 80),
		)
		// 空回答调试：打印消息角色序列，定位 LLM 为何给空答案
		if len(resp.ToolCalls) == 0 && resp.Content == "" {
			roles := make([]string, 0, len(messages))
			for _, m := range messages {
				roles = append(roles, string(m.Role))
			}
			e.logger.Warn("LLM 空回答调试",
				"run_id", run.ID, "msg_roles", roles,
				"last_content", truncate(lastMsgContent(messages), 120),
			)
		}
	}

	run.Steps++
	run.TokensSpent += resp.Usage.TotalTokens

	if len(resp.ToolCalls) == 0 {
		// 最终答案：持久化 assistant（无 tool_calls）
		run.Messages = append(run.Messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
		run.Answer = resp.Content
		run.State = agent.StateDone
		run.UpdatedAt = time.Now()
		e.persistRun(run)
		if e.logger != nil && resp.Content == "" {
			e.logger.Warn("LLM 返回空内容且无工具调用，视为无回答", "run_id", run.ID, "steps", run.Steps)
		}
		return run, nil
	}

	// 取第一个工具调用（一次一个，multigrid 实证）
	call := resp.ToolCalls[0]

	// 重复调用检测（指纹）
	fp := fingerprint(call)
	dup := false
	for _, seen := range run.SeenCalls {
		if seen == fp {
			dup = true
			break
		}
	}

	// 重复调用：不杀死 run（RAG 3.0 多跳可能重试同查询），
	// 而是注入提示让 LLM 换查询或基于已有结果回答（multigrid "search-again spiral" 引导）。
	// 注意：不持久化带 tool_call 的 assistant（否则 tool 结果缺失导致后续请求不匹配）。
	// 防死循环：MaxSteps 硬限 + SeenCalls 累积，循环必然撞预算失败。
	if dup {
		run.Messages = append(run.Messages, llm.Message{
			Role: llm.RoleUser,
			Content: "你刚刚已调用过工具 " + call.Name + " 且参数完全相同，请勿重复调用。" +
				"要么换一个不同的检索词/参数，要么基于已有结果直接回答。",
		})
		run.State = agent.StatePlanning
		run.UpdatedAt = time.Now()
		e.persistRun(run)
		return run, nil
	}

	// Plan 门控（go-steer/core-agent 实证）：写工具需先 record_plan
	if e.requiresPlan(call) && !e.planRecorded(run) {
		run.State = agent.StateFailed
		run.Error = fmt.Sprintf("工具 %s 需要先记录计划（plan-first）", call.Name)
		run.UpdatedAt = time.Now()
		e.persistRun(run)
		return run, nil
	}

	// 是否需要人类批准
	if e.tools != nil && e.tools.RequiresApproval(ctx, call) {
		// 持久化 assistant（带 tool_call），供批准后执行链关联
		run.Messages = append(run.Messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		run.PendingTool = &call
		run.State = agent.StateAwaitingApproval
		run.UpdatedAt = time.Now()
		e.persistRun(run)
		return run, nil
	}

	// 正常执行：持久化 assistant（带 tool_calls，OpenAI 标准关联 tool 结果）
	run.Messages = append(run.Messages, llm.Message{
		Role:      llm.RoleAssistant,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	})
	run.PendingTool = &call
	run.State = agent.StateActing
	run.UpdatedAt = time.Now()
	e.persistRun(run)
	return run, nil
}

// stepActing 执行状态：执行一个工具。
func (e *Engine) stepActing(ctx context.Context, run *agent.Run) (*agent.Run, error) {
	call := run.PendingTool
	if call == nil {
		run.State = agent.StatePlanning
		return run, nil
	}

	var result string
	if e.tools != nil {
		out, err := e.tools.Execute(ctx, *call)
		if err != nil {
			result = fmt.Sprintf("错误: %s", err.Error())
		} else {
			result = out
		}
	} else {
		result = "无工具执行器"
	}

	// 截断工具输出（multigrid 实证：无界工具输出=无界账单）
	if len(result) > e.cfg.ToolOutputCap {
		result = result[:e.cfg.ToolOutputCap] + "...(截断)"
	}

	// 记录指纹（防重复）
	run.SeenCalls = append(run.SeenCalls, fingerprint(*call))

	// 工具结果回传
	run.Messages = append(run.Messages, llm.Message{
		Role:       llm.RoleTool,
		Content:    result,
		ToolCallID: call.ID,
		Name:       call.Name,
	})

	run.PendingTool = nil
	run.State = agent.StatePlanning
	run.UpdatedAt = time.Now()
	e.persistRun(run)
	return run, nil
}

// Approve 人类批准。
func (e *Engine) Approve(_ context.Context, runID string, decision bool) (*agent.Run, error) {
	e.mu.Lock()
	run, ok := e.runs[runID]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("运行不存在: %s", runID)
	}
	if run.State != agent.StateAwaitingApproval {
		return nil, fmt.Errorf("运行不在等待批准状态: %s", run.State)
	}

	if decision {
		run.State = agent.StateActing
	} else {
		// 拒绝 = deny，反馈给 LLM
		run.Messages = append(run.Messages, llm.Message{
			Role:    llm.RoleTool,
			Content: "用户拒绝了该工具调用",
			Name:    run.PendingTool.Name,
		})
		run.PendingTool = nil
		run.State = agent.StatePlanning
	}
	run.UpdatedAt = time.Now()
	e.persistRun(run)
	return run, nil
}

// Steer 中途改方向。
func (e *Engine) Steer(_ context.Context, runID, msg string) error {
	e.mu.Lock()
	run, ok := e.runs[runID]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("运行不存在: %s", runID)
	}
	run.Messages = append(run.Messages, llm.Message{Role: llm.RoleUser, Content: msg})
	run.State = agent.StatePlanning
	run.UpdatedAt = time.Now()
	e.persistRun(run)
	return nil
}

// Abort 取消。
func (e *Engine) Abort(_ context.Context, runID string) error {
	e.mu.Lock()
	if _, ok := e.runs[runID]; !ok {
		e.mu.Unlock()
		return fmt.Errorf("运行不存在: %s", runID)
	}
	delete(e.runs, runID)
	e.mu.Unlock()
	if e.store != nil {
		_ = e.store.DeleteRun(runID)
	}
	return nil
}

// Resume 崩溃后续跑（内存实现：返回当前状态）。
func (e *Engine) Resume(_ context.Context, runID string) (*agent.Run, error) {
	e.mu.RLock()
	run, ok := e.runs[runID]
	e.mu.RUnlock()
	if ok {
		return run, nil
	}
	// 内存无：尝试从 DB 恢复（B5 崩溃续跑）
	if e.store != nil {
		loaded, err := e.store.LoadRun(runID)
		if err != nil {
			return nil, fmt.Errorf("运行不存在（内存 + DB）: %s: %w", runID, err)
		}
		e.mu.Lock()
		e.runs[runID] = loaded
		e.mu.Unlock()
		return loaded, nil
	}
	return nil, fmt.Errorf("运行不存在: %s", runID)
}

// Get 获取运行（内部用）。
func (e *Engine) Get(runID string) (*agent.Run, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	run, ok := e.runs[runID]
	return run, ok
}

// requiresPlan 判断工具是否需要 plan-first 门控。
// 原则（core-agent "gate everything by default" 的平衡版）：
//   - 只读工具（ReadOnly 元数据）不需要 plan（search/read 是安全的探索）
//   - 写/exec/破坏性工具需要 plan（先记录计划再执行）
//   - record_plan 是逃生阀，本身不受 gate
func (e *Engine) requiresPlan(call llm.ToolCall) bool {
	if !e.cfg.RequirePlan {
		return false
	}
	if call.Name == "record_plan" {
		return false // 逃生阀
	}
	// 依据元数据：只读工具放行（search_knowledge/read_file/search_files 等）
	if e.tools != nil && e.tools.IsReadOnly(call.Name) {
		return false
	}
	// 写/破坏性/未知工具：默认 gate（保守）
	return true
}

// planRecorded 判断是否已记录计划。
func (e *Engine) planRecorded(run *agent.Run) bool {
	return run.Plan != nil && run.Plan.Recorded
}

// toolsSchema 从工具执行器获取工具 schema（function calling）。
func (e *Engine) toolsSchema() []llm.ToolDef {
	if e.tools == nil {
		return nil
	}
	return e.tools.ToolDefs()
}

// newRunID 生成运行 ID。
func newRunID() string {
	h := sha256.Sum256([]byte(time.Now().String()))
	return "run_" + hex.EncodeToString(h[:4])
}

// fingerprint 工具调用指纹（防重复）。
func fingerprint(call llm.ToolCall) string {
	h := sha256.Sum256([]byte(call.Name + ":" + call.Arguments))
	return call.Name + ":" + hex.EncodeToString(h[:8])
}

// truncate 截断长字符串（日志调试用）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// lastMsgContent 取最后一条消息内容（调试日志用）。
func lastMsgContent(msgs []llm.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].Content
}

var _ = json.Marshal
