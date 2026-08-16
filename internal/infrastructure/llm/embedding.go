package llm

// embedding 客户端（模块 19：OpenAI 兼容 /v1/embeddings 端点）。
// 设计：通用 OpenAI 兼容 embedding 端点，阶段 2.1 接入编译管道。
// 支持任意 OpenAI 兼容端点（NVIDIA/OpenAI/ollama 等），不引第三方 SDK。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// EmbeddingClient 通用 OpenAI 兼容 embedding 客户端。
type EmbeddingClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewEmbeddingClient 创建 embedding 客户端。
func NewEmbeddingClient(baseURL, apiKey string, timeout time.Duration) *EmbeddingClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &EmbeddingClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// embeddingReq OpenAI embeddings 请求体。
type embeddingReq struct {
	Model          string `json:"model"`
	Input          any    `json:"input"` // string | []string
	EncodingFormat string `json:"encoding_format,omitempty"`
}

// embeddingResp OpenAI embeddings 响应体。
type embeddingResp struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed 生成向量（单文本）。
func (c *EmbeddingClient) Embed(ctx context.Context, model, text string) ([]float32, error) {
	vecs, err := c.EmbedBatch(ctx, model, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding 返回空")
	}
	return vecs[0], nil
}

// EmbedBatch 批量生成向量。
func (c *EmbeddingClient) EmbedBatch(ctx context.Context, model string, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingReq{
		Model: model,
		Input: texts,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var bodyBytes [512]byte
		n, _ := resp.Body.Read(bodyBytes[:])
		return nil, fmt.Errorf("embedding API: status=%d body=%s", resp.StatusCode, string(bodyBytes[:n]))
	}

	var result embeddingResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 embedding 响应失败: %w", err)
	}

	vecs := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		vecs[d.Index] = d.Embedding
	}
	return vecs, nil
}
