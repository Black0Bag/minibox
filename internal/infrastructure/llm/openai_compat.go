// Package llm 提供大模型供应商的 infrastructure 实现。
// 通用 OpenAI 兼容客户端：覆盖 deepseek/glm/nemotron/ollama 等所有 OpenAI 兼容端点。
package llm

import (
	"bufio"
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
	Model            string        `json:"model"`
	Messages         []chatMessage `json:"messages"`
	Stream           bool          `json:"stream"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	Stop             []string      `json:"stop,omitempty"`
	Tools            []chatTool    `json:"tools,omitempty"`
	ReasoningEffort  *string       `json:"reasoning_effort,omitempty"`
	// Anthropic 扩展思考（通过 OpenAI 兼容模式传递）
	Thinking *struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens,omitempty"`
	} `json:"thinking,omitempty"`
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
	Index    int              `json:"index,omitempty"`
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

	respBody, statusCode, contentType, err := c.do(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, llm.ClassifyError(statusCode, string(respBody))
	}
	if isEventStream(contentType, respBody) {
		return parseCompletionSSE(bytes.NewReader(respBody))
	}
	return parseCompletionJSON(respBody)
}

// parseCompletionJSON 将标准非流式 Chat Completions JSON 响应归一化。
func parseCompletionJSON(body []byte) (*llm.Response, error) {
	var resp chatCompletionResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 OpenAI JSON 响应失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI JSON 响应无 choices")
	}

	choice := resp.Choices[0]
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

// isEventStream 判断供应商是否以 SSE 形式返回 Chat Completions。
// 某些“OpenAI 兼容”供应商即使接收到 stream=false 仍返回 data: SSE；
// 此处仅做兼容解析，不改变标准 stream=true 的公开行为。
func isEventStream(contentType string, body []byte) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(body), []byte("data:"))
}

// parseCompletionSSE 聚合 data-only SSE Chat Completion chunks 为非流式响应。
func parseCompletionSSE(r io.Reader) (*llm.Response, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	out := &llm.Response{}
	toolCalls := make(map[string]*llm.ToolCall)
	toolIndexes := make(map[int]string)
	var toolOrder []string
	seenChunk := false
	seenDone := false

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			seenDone = true
			break
		}

		var chunk chatCompletionStreamResp
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("解析 OpenAI SSE chunk 失败: %w", err)
		}
		seenChunk = true
		if chunk.Model != "" {
			out.Model = chunk.Model
		}
		if chunk.Usage != nil {
			out.Usage = llm.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta
		out.Content += delta.Content
		out.Reasoning += delta.Reasoning
		if choice.FinishReason != nil {
			out.FinishReason = *choice.FinishReason
		}
		for _, tc := range delta.ToolCalls {
			callID := tc.ID
			if callID == "" && tc.Index >= 0 {
				callID = toolIndexes[tc.Index]
			}
			if callID == "" {
				return nil, fmt.Errorf("OpenAI SSE tool_call 缺少 id")
			}
			if tc.Index >= 0 {
				toolIndexes[tc.Index] = callID
			}
			acc, ok := toolCalls[callID]
			if !ok {
				acc = &llm.ToolCall{ID: callID, Name: tc.Function.Name}
				toolCalls[callID] = acc
				toolOrder = append(toolOrder, callID)
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			acc.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 OpenAI SSE 响应失败: %w", err)
	}
	if !seenChunk {
		return nil, fmt.Errorf("OpenAI SSE 响应无有效 chunk")
	}
	if !seenDone {
		return nil, fmt.Errorf("OpenAI SSE 响应缺少 [DONE]")
	}
	for _, id := range toolOrder {
		out.ToolCalls = append(out.ToolCalls, *toolCalls[id])
	}
	return out, nil
}

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

	// 思考强度映射：不同供应商适配不同参数
	// OpenAI/DeepSeek → reasoning_effort（low/medium/high）
	// Anthropic → thinking（extended + budget_tokens）
	// 通用降级：不支持时自动忽略
	var reasoningEffort *string
	var thinking *struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens,omitempty"`
	}
	if req.Thinking != llm.ThinkingNone && req.Thinking != "" {
		eff := string(req.Thinking)
		reasoningEffort = &eff
		// Anthropic extended thinking：当使用 claude 系列模型时启用
		if strings.Contains(req.Model, "claude") {
			reasoningEffort = nil // 不发送 reasoning_effort
			thinking = &struct {
				Type         string `json:"type"`
				BudgetTokens int    `json:"budget_tokens,omitempty"`
			}{
				Type: "enabled",
			}
			// 根据思考强度设置预算 token
			switch req.Thinking {
			case llm.ThinkingLow:
				thinking.BudgetTokens = 1024
			case llm.ThinkingMedium:
				thinking.BudgetTokens = 4096
			case llm.ThinkingHigh:
				thinking.BudgetTokens = 8192
			case llm.ThinkingXHigh:
				thinking.BudgetTokens = 16384
			default:
				thinking.BudgetTokens = 2048
			}
		}
	}

	body := chatCompletionReq{
		Model:            req.Model,
		Messages:         messages,
		Stream:           stream,
		MaxTokens:        req.MaxTokens,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
		Stop:             req.StopSequences,
		Tools:            tools,
		ReasoningEffort:  reasoningEffort,
		Thinking:         thinking,
	}

	// 默认模型回退：req.Model 为空时用供应商默认模型（域契约"空=用默认"）
	if body.Model == "" && c.defaultModel != "" {
		body.Model = c.defaultModel
	}

	return json.Marshal(body)
}

// do 发送 HTTP 请求。
func (c *OpenAICompat) do(ctx context.Context, path string, body []byte) ([]byte, int, string, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, "", fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if len(c.apiKeys) > 0 && c.apiKeys[0] != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKeys[0])
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Get("Content-Type"), err
	}
	return respBody, resp.StatusCode, resp.Header.Get("Content-Type"), nil
}
