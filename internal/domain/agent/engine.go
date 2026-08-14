// Package agent 定义 Agent 引擎。
// 设计：显式状态机（multigrid 2026 "Agent Loop Into State Machine" 实证）。
// 原则（ai-agents skill）：确定性控制流、有界工具、可审计状态、显式可序列化。
package agent

import (
	"context"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// State Agent 运行状态（5 状态，multigrid 实证）。
type State string

const (
	StatePlanning         State = "planning"          // 思考/计划（唯一花 token 决定下一步的状态）
	StateActing           State = "acting"            // 执行一个工具（一次一个，副作用有记录）
	StateAwaitingApproval State = "awaiting_approval" // 等人类批准（可停几天不花钱）
	StateAwaitingInput    State = "awaiting_input"    // 等人类补充信息
	StateDone             State = "done"
	StateFailed           State = "failed"
)

// Mode 运行模式（Plan/Build，F16）。
type Mode string

const (
	ModePlan  Mode = "plan"  // 只分析+出计划，禁写工具
	ModeBuild Mode = "build" // 用户确认后，写工具开放
)

// Run Agent 一次运行（可持久化，每步落 todo_items/内存，崩溃续跑）。
type Run struct {
	ID          string        `json:"id"`
	SessionID   string        `json:"session_id"`
	State       State         `json:"state"`
	Mode        Mode          `json:"mode"`
	Steps       int           `json:"steps"`         // 已走步数（预算）
	TokensSpent int           `json:"tokens_spent"`  // token 消耗
	Messages    []llm.Message `json:"messages"`      // 可续传的对话
	PendingTool *llm.ToolCall `json:"pending_tool,omitempty"` // 待执行工具
	Plan        *Plan         `json:"plan,omitempty"`        // Plan/Build 模式
	SeenCalls   []string      `json:"seen_calls"`            // 已执行工具指纹（防重复）
	Answer      string        `json:"answer,omitempty"`      // 最终答案
	Error       string        `json:"error,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Plan 计划（plan-first，go-steer/core-agent 实证）。
type Plan struct {
	Recorded bool   `json:"recorded"` // 是否已 record_plan
	Content  string `json:"content"`  // 计划内容
}

// MaxSteps 默认最大步数（硬限，LLM 不能覆盖）。
const MaxSteps = 20

// Config Agent 引擎配置。
type Config struct {
	MaxSteps       int   // 硬步数上限
	MaxTokens      int   // 硬 token 上限
	Mode           Mode  // 默认模式
	RequirePlan    bool  // 是否强制 plan-first（写工具需先 record_plan）
	ToolOutputCap  int   // 工具输出截断上限（默认 8000，multigrid 实证）
}

// Engine Agent 引擎接口。
type Engine interface {
	// Start 启动一次运行。
	Start(ctx context.Context, req Request) (*Run, error)

	// Step 状态机单步推进（持久化每次转换）。
	Step(ctx context.Context, runID string) (*Run, error)

	// Approve 人类批准（AWAITING_APPROVAL 状态）。
	Approve(ctx context.Context, runID string, decision bool) (*Run, error)

	// Steer 中途改方向（agentcore 实证）。
	Steer(ctx context.Context, runID, msg string) error

	// Abort 取消。
	Abort(ctx context.Context, runID string) error

	// Resume 崩溃后续跑。
	Resume(ctx context.Context, runID string) (*Run, error)
}

// Request 启动请求。
type Request struct {
	SessionID string
	Message   string   // 用户输入
	Mode      Mode     // 覆盖默认模式
	History   []llm.Message // 历史消息（可选）
}

// Dependencies Engine 依赖（composition root 注入）。
// 接口在消费方定义（Go 惯例，daveamit 实证）。
type Dependencies struct {
	LLM    llm.Provider      // LLM 供应商
	Memory memory.Store      // 知识库
	Tools  ToolExecutor      // 工具执行器（Phase 5 定义）
}

// ToolExecutor 工具执行接口（Phase 5 实现，这里定义窄接口）。
type ToolExecutor interface {
	// Execute 执行工具调用，返回结果文本。
	Execute(ctx context.Context, call llm.ToolCall) (string, error)
	// RequiresApproval 判断工具是否需要人类批准。
	RequiresApproval(ctx context.Context, call llm.ToolCall) bool
}
