package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainllm "github.com/Black0Bag/minibox/internal/domain/llm"
)

// TestComplete 验证非流式生成 + 归一化。
func TestComplete(t *testing.T) {
	// mock server
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 读取请求体供断言
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"你好，世界",
					"reasoning_content":"我在思考",
					"tool_calls":[]
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15},
			"model":"deepseek-chat"
		}`))
	}))
	defer srv.Close()

	c := NewOpenAICompat("test", srv.URL, []string{"sk-test"}, 10*time.Second)
	resp, err := c.Complete(context.Background(), domainllm.Request{
		Model: "deepseek-chat",
		Messages: []domainllm.Message{
			{Role: domainllm.RoleUser, Content: "你好"},
		},
		Thinking: domainllm.ThinkingMedium,
	})
	if err != nil {
		t.Fatalf("Complete 失败: %v", err)
	}

	// 验证归一化
	if resp.Content != "你好，世界" {
		t.Errorf("Content 错误: %q", resp.Content)
	}
	if resp.Reasoning != "我在思考" {
		t.Errorf("Reasoning 错误: %q", resp.Reasoning)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage 错误: %+v", resp.Usage)
	}

	// 验证请求体包含 reasoning_effort
	if !strings.Contains(gotBody, "reasoning_effort") {
		t.Errorf("请求体应包含 reasoning_effort: %s", gotBody)
	}
}

func TestCompleteUnexpectedSSE(t *testing.T) {
	var sawExplicitFalse bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("解析请求失败: %v", err)
		}
		sawExplicitFalse = !request.Stream
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"model\":\"sse-model\",\"choices\":[{\"delta\":{\"content\":\"你\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"model\":\"sse-model\",\"choices\":[{\"delta\":{\"reasoning_content\":\"思考\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"model\":\"sse-model\",\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAICompat("test", srv.URL, []string{"test-key"}, time.Second)
	resp, err := c.Complete(context.Background(), domainllm.Request{
		Model:    "sse-model",
		Messages: []domainllm.Message{{Role: domainllm.RoleUser, Content: "你好"}},
	})
	if err != nil {
		t.Fatalf("Complete SSE 回退失败: %v", err)
	}
	if !sawExplicitFalse {
		t.Fatal("非流式请求必须显式发送 stream=false")
	}
	if resp.Content != "你好" || resp.Reasoning != "思考" || resp.FinishReason != "stop" {
		t.Fatalf("SSE 聚合异常: %+v", resp)
	}
	if resp.Usage.TotalTokens != 5 || resp.Model != "sse-model" {
		t.Fatalf("SSE usage/model 异常: %+v", resp)
	}
}

func TestCompleteUnexpectedSSEToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"function\":{\"name\":\"search_knowledge\",\"arguments\":\"{\\\"query\\\":\"}}]},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"minibox\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAICompat("test", srv.URL, []string{"test-key"}, time.Second)
	resp, err := c.Complete(context.Background(), domainllm.Request{Model: "sse-model"})
	if err != nil {
		t.Fatalf("Complete SSE 工具调用回退失败: %v", err)
	}
	if resp.FinishReason != "tool_calls" || len(resp.ToolCalls) != 1 {
		t.Fatalf("工具调用数量或结束原因异常: %+v", resp)
	}
	if got := resp.ToolCalls[0]; got.ID != "call_1" || got.Name != "search_knowledge" || got.Arguments != `{"query":"minibox"}` {
		t.Fatalf("工具调用聚合异常: %+v", got)
	}
}

func TestCompleteUnexpectedSSERejectsTruncatedStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAICompat("test", srv.URL, []string{"test-key"}, time.Second)
	_, err := c.Complete(context.Background(), domainllm.Request{Model: "sse-model"})
	if err == nil || !strings.Contains(err.Error(), "缺少 [DONE]") {
		t.Fatalf("截断 SSE 应返回明确错误，实际: %v", err)
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{429, domainllm.ErrRateLimited},
		{401, domainllm.ErrAuthFailed},
		{403, domainllm.ErrAuthFailed},
		{400, domainllm.ErrBadRequest},
		{500, domainllm.ErrServerError},
		{503, domainllm.ErrServerError},
	}
	for _, tc := range cases {
		err := domainllm.ClassifyError(tc.code, "test")
		if err == nil {
			t.Fatalf("code %d: err 为 nil", tc.code)
		}
		if err.(interface{ Unwrap() error }).Unwrap() != tc.want {
			t.Errorf("code %d: 期望 %v, 实际 %v", tc.code, tc.want, err)
		}
	}
}

// TestModels 验证模型列表获取。
func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"list",
			"data":[
				{"id":"deepseek-chat","object":"model","created":1700000000,"owned_by":"deepseek"},
				{"id":"deepseek-reasoner","object":"model","created":1700000000,"owned_by":"deepseek"}
			]
		}`))
	}))
	defer srv.Close()

	c := NewOpenAICompat("test", srv.URL, []string{"sk-test"}, 10*time.Second)
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models 失败: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("期望 2 个模型，实际 %d", len(models))
	}
	if models[0].ID != "deepseek-chat" {
		t.Errorf("模型 ID 错误: %s", models[0].ID)
	}
}

// TestStream 验证流式解析。
func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"思考中\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAICompat("test", srv.URL, []string{"sk-test"}, 10*time.Second)
	events, err := c.Stream(context.Background(), domainllm.Request{Model: "deepseek-chat"})
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}

	var text, thinking string
	var done bool
	for ev := range events {
		switch ev.Type {
		case domainllm.StreamTextDelta:
			text += ev.Text
		case domainllm.StreamThinkingDelta:
			thinking += ev.Thinking
		case domainllm.StreamDone:
			done = true
		case domainllm.StreamError:
			t.Fatalf("流错误: %v", ev.Err)
		}
	}
	if text != "你好" {
		t.Errorf("流式文本错误: %q", text)
	}
	if thinking != "思考中" {
		t.Errorf("流式思考错误: %q", thinking)
	}
	if !done {
		t.Error("流未正常结束")
	}
}

var _ = json.Valid
