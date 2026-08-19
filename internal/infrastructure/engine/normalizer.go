// Package engine 的归一化层。
// 把 LLM 流式事件映射为前端统一事件（SSE agent.* 事件）。
// 设计：前端只认一套格式，不关心后端接的是 deepseek/glm/claude。
package engine

import (
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// SSE 事件类型常量（设计文档第 2 项 18 种事件）。
const (
	// 流式推理事件（7 种，已有）
	EventTextMessageContent = "agent.text_message_content"
	EventReasoningDelta    = "agent.reasoning_delta"
	EventToolCallArgs      = "agent.tool_call_args"
	EventTextMessageEnd    = "agent.text_message_end"
	EventRunError          = "agent.run_error"
	EventToolCallStart     = "agent.tool_call_start"
	EventToolCallResult    = "agent.tool_call_result"

	// 运行生命周期事件（5 种，补齐）
	EventTextMessageStart  = "agent.text_message_start"
	EventRunStarted        = "agent.run_started"
	EventRunFinished       = "agent.run_finished"
	EventStepStarted       = "agent.step_started"
	EventStepFinished      = "agent.step_finished"
	EventApprovalRequested = "agent.approval_requested"

	// 系统通知事件（6 种，补齐）
	EventCompileProgress = "system.compile_progress"
	EventCompileFinished = "system.compile_finished"
	EventCompileError    = "system.compile_error"
	EventUpgradeProgress = "system.upgrade_progress"
	EventDegradeNotify   = "system.degrade_notify"
)

// UIEvent 前端统一事件（对应第 2 项 SSE agent.* 事件）。
type UIEvent struct {
	Type       string `json:"type"`
	MessageID  string `json:"message_id,omitempty"`
	Delta      string `json:"delta,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Content    string `json:"content,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	State      string `json:"state,omitempty"`
	Step       int    `json:"step,omitempty"`
	Level      string `json:"level,omitempty"`
	Progress   int    `json:"progress,omitempty"`
}

// Normalizer 把 LLM 流式事件归一化为 UI 事件。
// 依据：chonk-ai 统一事件流 + go-llm-router 推理归一化。
type Normalizer struct {
	messageID string
}

// NewNormalizer 创建归一化器。
func NewNormalizer(messageID string) *Normalizer {
	return &Normalizer{messageID: messageID}
}

// Map 把单个 LLM 流式事件映射为 UI 事件列表。
// 一个 LLM 事件可能产生 0 或多个 UI 事件。
func (n *Normalizer) Map(ev llm.StreamEvent) []UIEvent {
	switch ev.Type {
	case llm.StreamTextDelta:
		return []UIEvent{{
			Type:      "agent.text_message_content",
			MessageID: n.messageID,
			Delta:     ev.Text,
		}}
	case llm.StreamThinkingDelta:
		return []UIEvent{{
			Type:      "agent.reasoning_delta",
			MessageID: n.messageID,
			Delta:     ev.Thinking,
		}}
	case llm.StreamToolCallDelta:
		return []UIEvent{{
			Type:       "agent.tool_call_args",
			ToolCallID: ev.ToolCallID,
			Delta:      ev.Text,
		}}
	case llm.StreamDone:
		return []UIEvent{{
			Type:      "agent.text_message_end",
			MessageID: n.messageID,
		}}
	case llm.StreamError:
		return []UIEvent{{
			Type:    "agent.run_error",
			Content: ev.Err.Error(),
			IsError: true,
		}}
	}
	return nil
}

// MapToolStart 生成工具调用开始事件。
func (n *Normalizer) MapToolStart(call llm.ToolCall) UIEvent {
	return UIEvent{
		Type:       "agent.tool_call_start",
		ToolCallID: call.ID,
		ToolName:   call.Name,
	}
}

// MapToolResult 生成工具结果事件。
func (n *Normalizer) MapToolResult(call llm.ToolCall, result string) UIEvent {
	return UIEvent{
		Type:       "agent.tool_call_result",
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    result,
	}
}
// MapTextMessageStart 生成消息开始事件（流式推理前发，前端可显示"正在输入"）。
func (n *Normalizer) MapTextMessageStart() UIEvent {
	return UIEvent{
		Type:      EventTextMessageStart,
		MessageID: n.messageID,
	}
}

// MapRunStarted 生成运行开始事件。
func MapRunStarted(runID string) UIEvent {
	return UIEvent{Type: EventRunStarted, RunID: runID}
}

// MapRunFinished 生成运行完成事件。
func MapRunFinished(runID string, state string, steps int) UIEvent {
	return UIEvent{Type: EventRunFinished, RunID: runID, State: state, Step: steps}
}

// MapStepStarted 生成步骤开始事件。
func MapStepStarted(runID string, step int) UIEvent {
	return UIEvent{Type: EventStepStarted, RunID: runID, Step: step}
}

// MapStepFinished 生成步骤完成事件。
func MapStepFinished(runID string, step int) UIEvent {
	return UIEvent{Type: EventStepFinished, RunID: runID, Step: step}
}

// MapApprovalRequested 生成需要人工批准事件。
func MapApprovalRequested(runID string, toolName string) UIEvent {
	return UIEvent{Type: EventApprovalRequested, RunID: runID, ToolName: toolName}
}

// MapCompileProgress 生成编译进度事件。
func MapCompileProgress(progress int) UIEvent {
	return UIEvent{Type: EventCompileProgress, Progress: progress}
}

// MapCompileFinished 生成编译完成事件。
func MapCompileFinished() UIEvent {
	return UIEvent{Type: EventCompileFinished}
}

// MapCompileError 生成编译错误事件。
func MapCompileError(errMsg string) UIEvent {
	return UIEvent{Type: EventCompileError, Content: errMsg, IsError: true}
}

// MapUpgradeProgress 生成升级进度事件。
func MapUpgradeProgress(progress int) UIEvent {
	return UIEvent{Type: EventUpgradeProgress, Progress: progress}
}

// MapDegradeNotify 生成降级通知事件。
func MapDegradeNotify(level string) UIEvent {
	return UIEvent{Type: EventDegradeNotify, Level: level}
}