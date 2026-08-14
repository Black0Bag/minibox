// Package engine 的归一化层。
// 把 LLM 流式事件映射为前端统一事件（SSE agent.* 事件）。
// 设计：前端只认一套格式，不关心后端接的是 deepseek/glm/claude。
package engine

import (
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// UIEvent 前端统一事件（对应第 2 项 SSE agent.* 事件）。
type UIEvent struct {
	Type      string      `json:"type"`      // agent.text_message_content / reasoning_delta / tool_call_start ...
	MessageID string      `json:"message_id,omitempty"`
	Delta     string      `json:"delta,omitempty"`
	ToolName  string      `json:"tool_name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Content   string      `json:"content,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
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
