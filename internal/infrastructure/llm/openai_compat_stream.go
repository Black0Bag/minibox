package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// chatCompletionStreamResp 流式响应的一个 chunk。
type chatCompletionStreamResp struct {
	Choices []struct {
		Delta struct {
			Content      string         `json:"content"`
			Reasoning    string         `json:"reasoning_content"` // DeepSeek 思考
			ToolCalls    []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

// Stream 流式生成，返回事件通道。
func (c *OpenAICompat) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	body, err := c.buildRequest(req, true)
	if err != nil {
		return nil, err
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("构建流式请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKeys[0])

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := readAllLimited(resp.Body)
		return nil, llm.ClassifyError(resp.StatusCode, string(respBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go c.parseStream(ctx, resp, events)
	return events, nil
}

// parseStream 解析 SSE 流。
func (c *OpenAICompat) parseStream(_ context.Context, resp *http.Response, events chan<- llm.StreamEvent) {
	defer func() { _ = resp.Body.Close() }()
	defer close(events)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 大行缓冲

	// 累积工具调用参数（分片到达）
	var toolAccum map[string]*struct {
		id   string
		name string
		args strings.Builder
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			// 发送最终 done（含累积的工具调用）
			events <- llm.StreamEvent{Type: llm.StreamDone}
			return
		}

		var chunk chatCompletionStreamResp
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // 跳过无法解析的 chunk
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		if delta.Reasoning != "" {
			events <- llm.StreamEvent{Type: llm.StreamThinkingDelta, Thinking: delta.Reasoning}
		}
		if delta.Content != "" {
			events <- llm.StreamEvent{Type: llm.StreamTextDelta, Text: delta.Content}
		}
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				if toolAccum == nil {
					toolAccum = make(map[string]*struct {
						id   string
						name string
						args strings.Builder
					})
				}
				acc, ok := toolAccum[tc.ID]
				if !ok {
					acc = &struct {
						id   string
						name string
						args strings.Builder
					}{id: tc.ID, name: tc.Function.Name}
					toolAccum[tc.ID] = acc
				}
				acc.args.WriteString(tc.Function.Arguments)
				events <- llm.StreamEvent{
					Type:       llm.StreamToolCallDelta,
					ToolCallID: tc.ID,
					Text:       tc.Function.Arguments,
				}
			}
		}

		// usage 或 finish 处理（在 done 后统一）
		_ = chunk
	}

	if err := scanner.Err(); err != nil {
		events <- llm.StreamEvent{Type: llm.StreamError, Err: fmt.Errorf("读取流失败: %w", err)}
		return
	}
}

// readAllLimited 读取响应体（限长，防内存耗尽）。
func readAllLimited(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if len(buf) > 10*1024*1024 {
			return buf, fmt.Errorf("响应体过大")
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}
