// Package subagent 定义 subagent 引擎。
// 设计：Orchestrator-Worker 拓扑（BackendBytes 2026 实证）。
// 核心：失败是 value 不是 exception（errgroup return error 会杀兄弟）。
package subagent

import (
	"context"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// Task 交给 subagent 的工作单元。
type Task struct {
	AgentID   string `json:"agent_id"`  // 哪个 subagent 执行
	Objective string `json:"objective"` // 拆解后的子任务
	MaxTokens int    `json:"max_tokens"` // 预算上限
}

// Result subagent 返回结果。
// 失败走 Error 字段（value），不返回 error 杀兄弟。
type Result struct {
	AgentID string        `json:"agent_id"`
	Content string        `json:"content"`
	Tokens  int           `json:"tokens"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"` // 失败是 value
}

// Agent subagent 契约（BackendBytes 实证）。
type Agent interface {
	ID() string
	Run(ctx context.Context, t Task) (Result, error)
}

// Mode subagent 执行模式（agentcore 实证）。
type Mode string

const (
	// ModeSingle 一个 subagent 干一件活。
	ModeSingle Mode = "single"
	// ModeParallel 并行 fan-out。
	ModeParallel Mode = "parallel"
	// ModeChain 串行，前一个结果给下一个。
	ModeChain Mode = "chain"
	// ModeBackground 异步，立即返回。
	ModeBackground Mode = "background"
)

// Config Orchestrator 配置。
type Config struct {
	MaxConcurrent int           // 并发上限（errgroup.SetLimit）
	AgentTimeout  time.Duration // 单个 subagent 超时
	TokenBudget   int           // 总 token 预算
}

// RunReport 调度报告。
type RunReport struct {
	Results     []Result `json:"results"`
	Failures    []Failure `json:"failures"`
	TokensSpent int       `json:"tokens_spent"`
	Elapsed     time.Duration `json:"elapsed"`
}

// Failure subagent 失败记录。
type Failure struct {
	AgentID string `json:"agent_id"`
	Error   string `json:"error"`
}

// Orchestrator subagent 调度器接口。
type Orchestrator interface {
	// Dispatch 把计划扇出给 subagent 并发执行并合成结果。
	Dispatch(ctx context.Context, plan []Task) (RunReport, error)

	// RunSingle 单 subagent 执行。
	RunSingle(ctx context.Context, t Task) (Result, error)

	// RunChain 串行执行（前一个结果给下一个）。
	RunChain(ctx context.Context, tasks []Task) (RunReport, error)

	// RunBackground 后台异步执行（立即返回）。
	RunBackground(ctx context.Context, t Task) (<-chan Result, error)
}

// AgentConfig subagent 定义（独立人格 + 独立模型 + skill，QB10-QB13）。
type AgentConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	SystemPrompt string   `json:"system_prompt"` // 独立人格
	Model        string   `json:"model,omitempty"` // 独立模型（空=继承主 agent）
	Skills       []string `json:"skills,omitempty"` // 可调 skill
	Tools        []string `json:"tools,omitempty"` // 工具白名单
	MaxTurns     int      `json:"max_turns,omitempty"`
}

// Factory subagent 工厂（composition root 装配）。
// 依赖注入窄接口（消费方定义）。
type Factory interface {
	// Build 根据配置构建 subagent。
	Build(cfg AgentConfig) (Agent, error)
}

// LLMProvider subagent 依赖的 LLM 接口（窄接口）。
type LLMProvider interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}
