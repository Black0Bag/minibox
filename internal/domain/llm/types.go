// Package llm 定义大模型供应商的统一接口。
// 设计：单接口 + Normalizer 内部消化各家差异（agentsdk-go v2 实证）。
// 不绑死任何模型，支持任意 OpenAI 兼容端点（deepseek/glm/nemotron/ollama 等）。
package llm

import (
	"context"
	"fmt"
)

// Provider 是 LLM 供应商统一接口。
// 实现：infrastructure/llm 下的 OpenAI 兼容客户端等。
type Provider interface {
	// Name 供应商标识。
	Name() string

	// Complete 非流式生成。
	Complete(ctx context.Context, req Request) (*Response, error)

	// Stream 流式生成，返回事件通道。
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)

	// Models 获取供应商可用模型列表（能力识别，Q4A2）。
	Models(ctx context.Context) ([]ModelInfo, error)
}

// ThinkingLevel 思考强度（5 档）。
// ThinkingLevel 思考强度（5 档）。
type ThinkingLevel string

const (
	// ThinkingNone 不思考。
	ThinkingNone ThinkingLevel = "none"
	// ThinkingLow 低强度思考。
	ThinkingLow ThinkingLevel = "low"
	// ThinkingMedium 中强度思考。
	ThinkingMedium ThinkingLevel = "medium"
	// ThinkingHigh 高强度思考。
	ThinkingHigh ThinkingLevel = "high"
	// ThinkingXHigh 极强思考。
	ThinkingXHigh ThinkingLevel = "xhigh"
)

// MessageRole 消息角色。
type MessageRole string

const (
	// RoleSystem 系统提示。
	RoleSystem MessageRole = "system"
	// RoleUser 用户输入。
	RoleUser MessageRole = "user"
	// RoleAssistant 助手回复。
	RoleAssistant MessageRole = "assistant"
	// RoleTool 工具结果。
	RoleTool MessageRole = "tool"
)

// Message 对话消息。
type Message struct {
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"` // assistant 消息附带的工具调用（OpenAI 标准）
}

// ToolDef 工具定义（供 LLM 调用）。
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef 函数定义。
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Request 生成请求。
type Request struct {
	Model            string
	Feature          Feature       // B6 功能标识（如 "agent"/"pref_extract"/"subagent"），Router 据此选择模型
	Messages         []Message
	Thinking         ThinkingLevel
	Tools            []ToolDef
	MaxTokens        *int
	Temperature      *float64
	TopP             *float64       `json:"top_p,omitempty"`             // 核采样（0.0~1.0）
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"` // 频率惩罚（-2.0~2.0）
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`  // 存在惩罚（-2.0~2.0）
	StopSequences    []string       `json:"stop_sequences,omitempty"`    // 停止序列
}

// ToolCall 工具调用（模型请求执行的工具）。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// Usage token 使用情况。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Response 生成响应（归一化后的统一结构）。
type Response struct {
	Content      string     `json:"content"`
	Reasoning    string     `json:"reasoning"` // 思考过程（思考折叠卡用）
	ToolCalls    []ToolCall `json:"tool_calls"`
	Usage        Usage      `json:"usage"`
	FinishReason string     `json:"finish_reason"`
	Model        string     `json:"model"`
}

// StreamEventType 流式事件类型。
type StreamEventType int

const (
	// StreamTextDelta 文本增量。
	StreamTextDelta StreamEventType = iota
	// StreamThinkingDelta 思考增量。
	StreamThinkingDelta
	// StreamToolCallDelta 工具调用参数增量。
	StreamToolCallDelta
	// StreamDone 流结束。
	StreamDone
	// StreamError 错误。
	StreamError
)

// StreamEvent 流式事件（归一化）。
type StreamEvent struct {
	Type         StreamEventType
	Text         string    // TextDelta 时
	Thinking     string    // ThinkingDelta 时
	ToolCallID   string    // ToolCallDelta 时
	ToolCall     *ToolCall // Done 时的完整工具调用
	Usage        *Usage    // Done 时
	FinishReason string    // Done 时
	Err          error     // Error 时
}

// Feature 功能标识（B6 功能级模型独立配置）。
type Feature string

const (
	// FeatureAgent 对话/Agent 引擎。
	FeatureAgent Feature = "agent"
	// FeaturePrefExtract 偏好蒸馏。
	FeaturePrefExtract Feature = "pref_extract"
	// FeatureSubagent 子代理。
	FeatureSubagent Feature = "subagent"
	// FeatureMemoryCompile 记忆编译/压缩/总结。
	FeatureMemoryCompile Feature = "memory_compile"
	// FeatureMemoryEmbed 记忆嵌入（向量化）。
	FeatureMemoryEmbed Feature = "memory_embed"
	// FeatureSoulGenerate 灵魂文件生成。
	FeatureSoulGenerate Feature = "soul_generate"
	// FeatureProfileAnalyze 用户画像分析。
	FeatureProfileAnalyze Feature = "profile_analyze"
	// FeatureToolDecision 工具决策（选哪个工具）。
	FeatureToolDecision Feature = "tool_decision"
	// FeatureKnowledgeCompile 知识编译。
	FeatureKnowledgeCompile Feature = "knowledge_compile"
)

// FeatureConfig 单个功能→模型映射（B6）。
type FeatureConfig struct {
	Feature  Feature `json:"feature"`  // 功能标识
	Provider string  `json:"provider"` // 供应商标识（空=使用默认）
	Model    string  `json:"model"`    // 模型ID（空=使用默认）
}

// Validate 校验功能配置是否合法。
func (fc FeatureConfig) Validate() error {
	switch fc.Feature {
	case FeatureAgent, FeaturePrefExtract, FeatureSubagent,
		FeatureMemoryCompile, FeatureMemoryEmbed, FeatureSoulGenerate,
		FeatureProfileAnalyze, FeatureToolDecision, FeatureKnowledgeCompile:
		return nil
	default:
		return fmt.Errorf("未知功能标识: %s", fc.Feature)
	}
}

// FeatureModels 功能级模型配置集合。
type FeatureModels struct {
	Configs map[Feature]FeatureConfig `json:"configs"`
}

// Get 获取指定功能的模型配置，不存在时返回空 FeatureConfig（使用默认）。
func (fm *FeatureModels) Get(feature Feature) FeatureConfig {
	if fm == nil || fm.Configs == nil {
		return FeatureConfig{}
	}
	return fm.Configs[feature]
}

// Set 设置指定功能的模型配置。
func (fm *FeatureModels) Set(fc FeatureConfig) {
	if fm.Configs == nil {
		fm.Configs = make(map[Feature]FeatureConfig)
	}
	fm.Configs[fc.Feature] = fc
}

// ModelInfo 模型能力信息（能力识别结果，Q4A2）。
type ModelInfo struct {
	ID                    string   `json:"id"`
	ContextLength         int      `json:"context_length"`
	MaxOutputTokens       int      `json:"max_output_tokens"`
	SupportsTools         bool     `json:"supports_tools"`
	SupportsThinking      bool     `json:"supports_thinking"`
	SupportedEfforts      []string `json:"supported_efforts,omitempty"` // 支持的思考强度
	SupportsVision        bool     `json:"supports_vision"`
	SupportsStructuredOut bool     `json:"supports_structured_out"`
	Provenance            string   `json:"provenance"` // api / probe / models_dev / manual
}
