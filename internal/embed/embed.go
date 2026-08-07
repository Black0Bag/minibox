package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client 远程 Embedding API 客户端（OpenAI 兼容格式）
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// New 创建 embedding 客户端
func New(baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

type embedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed 将文本列表向量化，返回与输入等长的向量列表
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedReq{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("序列化请求: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构建请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 embedding API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API 状态 %d: %s", resp.StatusCode, string(data))
	}
	var er embedResp
	if err := json.Unmarshal(data, &er); err != nil {
		return nil, fmt.Errorf("解析响应: %w", err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("embedding API 错误: %s", er.Error.Message)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("返回向量数 %d 与输入数 %d 不符", len(er.Data), len(texts))
	}
	out := make([][]float32, len(er.Data))
	for i, d := range er.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// Model 返回配置的模型名
func (c *Client) Model() string { return c.model }
