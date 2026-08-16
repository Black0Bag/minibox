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
	dimensions int // 输出维度（0 表示不限制，模型默认）
}

// NewEmbeddingClient 创建 embedding 客户端。
func NewEmbeddingClient(baseURL, apiKey string, timeout time.Duration, dimensions int) *EmbeddingClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &EmbeddingClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		dimensions: dimensions,
	}
}

// embeddingReq OpenAI embeddings 请求体。
type embeddingReq struct {
	Model          string `json:"model"`
	Input          any    `json:"input"` // string | []string
	EncodingFormat string `json:"encoding_format,omitempty"`
	InputType      string `json:"input_type,omitempty"` // NVIDIA asymmetric 模型：passage=索引 / query=查询
	Dimensions     int    `json:"dimensions,omitempty"` // 输出维度（支持动态维度的模型可降维）
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

// EmbeddingType 向量用途（asymmetric 模型区分 passage/query）。
type EmbeddingType string

const (
	// EmbedTypePassage 索引文档向量（NVIDIA asymmetric 模型）。
	EmbedTypePassage EmbeddingType = "passage"
	// EmbedTypeQuery 查询向量（NVIDIA asymmetric 模型）。
	EmbedTypeQuery EmbeddingType = "query"
)

// Embed 生成向量（单文本，passage 模式，索引用）。
func (c *EmbeddingClient) Embed(ctx context.Context, model, text string) ([]float32, error) {
	vecs, err := c.EmbedTyped(ctx, model, []string{text}, EmbedTypePassage)
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding 返回空")
	}
	return vecs[0], nil
}

// EmbedBatch 批量生成向量（passage 模式，索引用）。
func (c *EmbeddingClient) EmbedBatch(ctx context.Context, model string, texts []string) ([][]float32, error) {
	return c.EmbedTyped(ctx, model, texts, EmbedTypePassage)
}

// EmbedQuery 生成查询向量（query 模式，检索用）。
func (c *EmbeddingClient) EmbedQuery(ctx context.Context, model, text string) ([]float32, error) {
	vecs, err := c.EmbedTyped(ctx, model, []string{text}, EmbedTypeQuery)
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding 返回空")
	}
	return vecs[0], nil
}

// EmbedTyped 按指定类型批量生成向量。
// NVIDIA asymmetric 模型（llama-nemotron-embed 等）要求：索引用 passage，查询用 query。
func (c *EmbeddingClient) EmbedTyped(ctx context.Context, model string, texts []string, inputType EmbeddingType) ([][]float32, error) {
	body, err := json.Marshal(embeddingReq{
		Model:      model,
		Input:      texts,
		InputType:  string(inputType),
		Dimensions: c.dimensions,
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
