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

	mu   sync.RWMutex
	runs map[string]*agent.Run
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

// Start 启动一次运行。
func (e *Engine) Start(ctx context.Context, req agent.Request) (*agent.Run, error) {
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
		Model:    "", // 用默认模型
		Messages: messages,
		Tools:    e.toolsSchema(),
	})
	if err != nil {
		run.State = agent.StateFailed
		run.Error = err.Error()
		run.UpdatedAt = time.Now()
		return run, nil
	}

	run.Steps++
	run.TokensSpent += resp.Usage.TotalTokens
	run.Messages = append(run.Messages, llm.Message{
		Role:    llm.RoleAssistant,
		Content: resp.Content,
	})
	run.UpdatedAt = time.Now()

	if len(resp.ToolCalls) == 0 {
		// 最终答案
		run.Answer = resp.Content
		run.State = agent.StateDone
		return run, nil
	}

	// 取第一个工具调用（一次一个，multigrid 实证）
	call := resp.ToolCalls[0]

	// 重复调用检测（指纹）
	fp := fingerprint(call)
	for _, seen := range run.SeenCalls {
		if seen == fp {
			run.State = agent.StateFailed
			run.Error = fmt.Sprintf("重复工具调用: %s", call.Name)
			return run, nil
		}
	}

	run.PendingTool = &call

	// Plan 门控（go-steer/core-agent 实证）：写工具需先 record_plan
	if e.requiresPlan(call) && !e.planRecorded(run) {
		run.State = agent.StateFailed
		run.Error = fmt.Sprintf("工具 %s 需要先记录计划（plan-first）", call.Name)
		return run, nil
	}

	// 是否需要人类批准
	if e.tools != nil && e.tools.RequiresApproval(ctx, call) {
		run.State = agent.StateAwaitingApproval
		return run, nil
	}

	run.State = agent.StateActing
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
	return run, nil
}

// Approve 人类批准。
func (e *Engine) Approve(ctx context.Context, runID string, decision bool) (*agent.Run, error) {
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
	return run, nil
}

// Steer 中途改方向。
func (e *Engine) Steer(ctx context.Context, runID, msg string) error {
	e.mu.Lock()
	run, ok := e.runs[runID]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("运行不存在: %s", runID)
	}
	run.Messages = append(run.Messages, llm.Message{Role: llm.RoleUser, Content: msg})
	run.State = agent.StatePlanning
	run.UpdatedAt = time.Now()
	return nil
}

// Abort 取消。
func (e *Engine) Abort(ctx context.Context, runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.runs[runID]; !ok {
		return fmt.Errorf("运行不存在: %s", runID)
	}
	delete(e.runs, runID)
	return nil
}

// Resume 崩溃后续跑（内存实现：返回当前状态）。
func (e *Engine) Resume(ctx context.Context, runID string) (*agent.Run, error) {
	e.mu.RLock()
	run, ok := e.runs[runID]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("运行不存在: %s", runID)
	}
	return run, nil
}

// Get 获取运行（内部用）。
func (e *Engine) Get(runID string) (*agent.Run, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	run, ok := e.runs[runID]
	return run, ok
}

// requiresPlan 判断工具是否需要 plan-first 门控。
func (e *Engine) requiresPlan(call llm.ToolCall) bool {
	if !e.cfg.RequirePlan {
		return false
	}
	// 写/exec 类工具需 plan；读工具不需要（core-agent 实证）
	switch call.Name {
	case "write_file", "edit_file", "delete_file", "bash", "spawn_agent", "record_plan":
		return call.Name != "record_plan" // record_plan 是逃生阀
	default:
		// 保守：未知工具默认 gate（core-agent "gate everything by default"）
		return true
	}
}

// planRecorded 判断是否已记录计划。
func (e *Engine) planRecorded(run *agent.Run) bool {
	return run.Plan != nil && run.Plan.Recorded
}

// toolsSchema 从工具执行器获取工具 schema（Phase 5 接入）。
// 当前返回空（Phase 5 补全）。
func (e *Engine) toolsSchema() []llm.ToolDef {
	// Phase 5 从 Registry 获取
	return nil
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

var _ = json.Marshal
