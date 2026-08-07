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
)

// Message 对话消息
type Message struct {
	Role    string `json:"role"` // system / user / assistant
	Content string `json:"content"`
}

// Options 调用选项
type Options struct {
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

// Client 多供应商 LLM 客户端（OpenAI 兼容 chat/completions）
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// New 创建 LLM 客户端
func New(baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Model 返回模型名
func (c *Client) Model() string { return c.model }

// BaseURL 返回 API 地址（用于日志/状态，key 不暴露）
func (c *Client) BaseURL() string { return c.baseURL }

type chatReq struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type chatResp struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat 非流式对话
func (c *Client) Chat(ctx context.Context, messages []Message, opts Options) (string, error) {
	req := chatReq{Model: c.model, Messages: messages}
	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		req.Temperature = opts.Temperature
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("序列化请求: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构建请求: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("调用 LLM API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM API 状态 %d: %s", resp.StatusCode, string(data))
	}
	var cr chatResp
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", fmt.Errorf("解析响应: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("LLM API 错误: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("LLM 无输出")
	}
	return cr.Choices[0].Message.Content, nil
}

// ============ 流式对话（M3）============

// StreamChunk 流式输出块
type StreamChunk struct {
	Type string // thinking / answer
	Text string
	Done bool
}

type streamResp struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
}

// ChatStream 流式对话：返回 chunk 通道（调用方负责消费完）
func (c *Client) ChatStream(ctx context.Context, messages []Message, opts Options) (<-chan StreamChunk, error) {
	req := chatReq{Model: c.model, Messages: messages, Stream: true}
	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		req.Temperature = opts.Temperature
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构建请求: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	// 流式需要更长超时
	client := *c.http
	client.Timeout = 300 * time.Second
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("调用 LLM API: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM API 状态 %d: %s", resp.StatusCode, string(data))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				ch <- StreamChunk{Type: "answer", Done: true}
				return
			}
			var sr streamResp
			if err := json.Unmarshal([]byte(data), &sr); err != nil {
				continue
			}
			if len(sr.Choices) == 0 {
				continue
			}
			delta := sr.Choices[0].Delta
			if delta.ReasoningContent != "" {
				ch <- StreamChunk{Type: "thinking", Text: delta.ReasoningContent}
			}
			if delta.Content != "" {
				ch <- StreamChunk{Type: "answer", Text: delta.Content}
			}
		}
	}()
	return ch, nil
}
