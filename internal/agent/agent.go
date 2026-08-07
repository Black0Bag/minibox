package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Black0Bag/minibox/internal/llm"
	"github.com/Black0Bag/minibox/internal/tools"
)

// Agent 精简 AgentLoop：LLM → 工具决策 → 执行 → 观察 → 循环 → 最终回答
type Agent struct {
	llm     *llm.Client
	tools   *tools.Registry
	maxIter int
}

// New 创建 Agent
func New(llmClient *llm.Client, reg *tools.Registry, maxIter int) *Agent {
	if maxIter <= 0 {
		maxIter = 3
	}
	return &Agent{llm: llmClient, tools: reg, maxIter: maxIter}
}

// Run 执行一轮任务（带工具循环），返回最终回答
func (a *Agent) Run(ctx context.Context, messages []llm.Message) (string, error) {
	resp := ""
	for i := 0; i < a.maxIter; i++ {
		msgs := make([]llm.Message, 0, len(messages)+1)
		msgs = append(msgs, messages...)
		msgs = append(msgs, llm.Message{Role: "system", Content: a.tools.LLMSpecs()})

		out, err := a.llm.Chat(ctx, msgs, llm.Options{MaxTokens: 2000, Temperature: 0.7})
		if err != nil {
			return "", fmt.Errorf("LLM 调用: %w", err)
		}
		resp = out

		name, args, ok := tools.ParseToolCall(out)
		if !ok {
			// 无工具调用，清理可能的残留标记后返回
			return tools.CleanToolMarkers(out), nil
		}
		result, err := a.tools.Run(ctx, name, args)
		if err != nil {
			result = "工具执行错误：" + err.Error()
		}
		// 工具结果回填，继续循环（ReAct：Thought → Action → Observation）
		messages = append(messages,
			llm.Message{Role: "assistant", Content: out},
			llm.Message{Role: "user", Content: "工具「" + name + "」返回结果：" + result + "\n请直接给出最终答案，绝对不要再输出任何 <tool> 工具调用标记。"},
		)
	}
	return resp, nil
}

// ToolSpecs 返回工具说明（调试用）
func (a *Agent) ToolSpecs() string {
	return strings.Join(a.tools.List(), ", ")
}
