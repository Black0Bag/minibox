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
)

// EmbeddingClient is a small OpenAI-compatible embeddings client.
type EmbeddingClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	dimensions int
	maxRetries int
	backoff    time.Duration
}

// NewEmbeddingClient creates a client. baseURL may be either a service root or a /v1 root.
func NewEmbeddingClient(baseURL, apiKey string, timeout time.Duration, dimensions int) *EmbeddingClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &EmbeddingClient{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: &http.Client{Timeout: timeout}, dimensions: dimensions, maxRetries: 2, backoff: 100 * time.Millisecond}
}

type embeddingReq struct {
	Model     string `json:"model"`
	Input     any    `json:"input"`
	InputType string `json:"input_type,omitempty"`
	// Dimensions is only sent when the configured model/service is known to support it.
	Dimensions *int `json:"dimensions,omitempty"`
}
type embeddingResp struct {
	Data []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

type EmbeddingType string

const (
	EmbedTypePassage EmbeddingType = "passage"
	EmbedTypeQuery   EmbeddingType = "query"
)

func (c *EmbeddingClient) Embed(ctx context.Context, model, text string) ([]float32, error) {
	vecs, err := c.EmbedTyped(ctx, model, []string{text}, EmbedTypePassage)
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embedding 返回数量异常: got=%d want=1", len(vecs))
	}
	return vecs[0], nil
}
func (c *EmbeddingClient) EmbedBatch(ctx context.Context, model string, texts []string) ([][]float32, error) {
	return c.EmbedTyped(ctx, model, texts, EmbedTypePassage)
}
func (c *EmbeddingClient) EmbedQuery(ctx context.Context, model, text string) ([]float32, error) {
	vecs, err := c.EmbedTyped(ctx, model, []string{text}, EmbedTypeQuery)
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embedding 返回数量异常: got=%d want=1", len(vecs))
	}
	return vecs[0], nil
}

func embeddingEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/embeddings") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/embeddings"
	}
	return baseURL + "/v1/embeddings"
}

// requestDimensions returns a dimension override only for models known to use
// Matryoshka-style dynamic dimensions. Generic models such as BGE-M3 use their
// native output size unless the caller later adds an explicit capability policy.
func (c *EmbeddingClient) requestDimensions(model string) *int {
	if c.dimensions <= 0 || strings.Contains(strings.ToLower(model), "bge-m3") {
		return nil
	}
	dim := c.dimensions
	return &dim
}

// EmbedTyped generates embeddings for a batch using the requested query/passage mode.
func (c *EmbeddingClient) EmbedTyped(ctx context.Context, model string, texts []string, inputType EmbeddingType) ([][]float32, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("embedding model 不能为空")
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("embedding input 不能为空")
	}
	body, err := json.Marshal(embeddingReq{
		Model:      model,
		Input:      texts,
		InputType:  string(inputType),
		Dimensions: c.requestDimensions(model),
	})
	if err != nil {
		return nil, fmt.Errorf("编码 embedding 请求失败: %w", err)
	}
	endpoint := embeddingEndpoint(c.baseURL)
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if reqErr != nil {
			return nil, fmt.Errorf("创建 embedding 请求失败: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			lastErr = fmt.Errorf("embedding 请求失败: %w", doErr)
		} else {
			responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			resp.Body.Close()
			if readErr != nil {
				lastErr = fmt.Errorf("读取 embedding 响应失败: %w", readErr)
			} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("embedding API: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
				if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
					return nil, lastErr
				}
			} else {
				var result embeddingResp
				if err := json.Unmarshal(responseBody, &result); err != nil {
					return nil, fmt.Errorf("解析 embedding 响应失败: %w", err)
				}
				if len(result.Data) != len(texts) {
					return nil, fmt.Errorf("embedding 返回数量异常: got=%d want=%d", len(result.Data), len(texts))
				}
				vecs := make([][]float32, len(texts))
				for _, item := range result.Data {
					if item.Index < 0 || item.Index >= len(texts) {
						return nil, fmt.Errorf("embedding index 越界: %d", item.Index)
					}
					if len(item.Embedding) == 0 || (c.dimensions > 0 && len(item.Embedding) != c.dimensions) {
						return nil, fmt.Errorf("embedding 维度异常: got=%d want=%d", len(item.Embedding), c.dimensions)
					}
					vecs[item.Index] = item.Embedding
				}
				return vecs, nil
			}
		}
		if attempt < c.maxRetries {
			timer := time.NewTimer(c.backoff * time.Duration(1<<attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, lastErr
}
