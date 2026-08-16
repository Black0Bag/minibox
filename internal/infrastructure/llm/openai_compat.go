// Package llm 提供大模型供应商的 infrastructure 实现。
// 通用 OpenAI 兼容客户端：覆盖 deepseek/glm/nemotron/ollama 等所有 OpenAI 兼容端点。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// OpenAICompat 是通用 OpenAI 兼容客户端。
// 所有 OpenAI 兼容端点（/v1/chat/completions）都能用。
type OpenAICompat struct {
	name         string
	baseURL      string
	apiKeys      []string
	httpClient   *http.Client
	defaultModel string // 默认模型（req.Model 为空时回退，域契约"空=用默认"）
}

// OpenAICompatOption 配置选项。
type OpenAICompatOption func(*OpenAICompat)

// WithDefaultModel 设置默认模型（req.Model 为空时回退）。
func WithDefaultModel(model string) OpenAICompatOption {
	return func(c *OpenAICompat) { c.defaultModel = model }
}

// NewOpenAICompat 创建 OpenAI 兼容客户端。
func NewOpenAICompat(name, baseURL string, apiKeys []string, timeout time.Duration, opts ...OpenAICompatOption) *OpenAICompat {
	c := &OpenAICompat{
		name:    name,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKeys: apiKeys,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name 返回供应商标识。
func (c *OpenAICompat) Name() string { return c.name }

// chatCompletionReq OpenAI chat/completions 请求体。
type chatCompletionReq struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Stream          bool          `json:"stream,omitempty"`
	MaxTokens       *int          `json:"max_tokens,omitempty"`
	Temperature     *float64      `json:"temperature,omitempty"`
	Tools           []chatTool    `json:"tools,omitempty"`
	ReasoningEffort *string       `json:"reasoning_effort,omitempty"`
}

// chatMessage 消息。
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

// chatToolCall 消息内嵌的工具调用（OpenAI 标准：assistant 消息携带）。
type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatTool 工具定义。
type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// chatCompletionResp OpenAI chat/completions 响应体。
type chatCompletionResp struct {
	Choices []struct {
		Message struct {
			Role      string         `json:"role"`
			Content   string         `json:"content"`
			Reasoning string         `json:"reasoning_content"` // DeepSeek 等用此字段
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

// Complete 非流式生成。
func (c *OpenAICompat) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	body, err := c.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := c.do(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, llm.ClassifyError(statusCode, string(respBody))
	}

	var resp chatCompletionResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应失败: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI 响应无 choices")
	}
	choice := resp.Choices[0]

	// 归一化输出
	out := &llm.Response{
		Content:      choice.Message.Content,
		Reasoning:    choice.Message.Reasoning,
		FinishReason: choice.FinishReason,
		Model:        resp.Model,
		Usage: llm.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return out, nil
}

// buildRequest 构建 chat/completions 请求体。
func (c *OpenAICompat) buildRequest(req llm.Request, stream bool) ([]byte, error) {
	messages := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		cm := chatMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		// assistant 消息的 tool_calls 必须携带（OpenAI 标准：tool 结果需关联 assistant tool_call）
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = make([]chatToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: chatToolCallFunc{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		messages = append(messages, cm)
	}

	tools := make([]chatTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, chatTool{
			Type: t.Type,
			Function: chatFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}

	var reasoningEffort *string
	if req.Thinking != llm.ThinkingNone && req.Thinking != "" {
		eff := string(req.Thinking)
		reasoningEffort = &eff
	}

	body := chatCompletionReq{
		Model:           req.Model,
		Messages:        messages,
		Stream:          stream,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		Tools:           tools,
		ReasoningEffort: reasoningEffort,
	}

	// 默认模型回退：req.Model 为空时用供应商默认模型（域契约"空=用默认"）
	if body.Model == "" && c.defaultModel != "" {
		body.Model = c.defaultModel
	}

	return json.Marshal(body)
}

// do 发送 HTTP 请求。
func (c *OpenAICompat) do(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKeys[0])

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}
