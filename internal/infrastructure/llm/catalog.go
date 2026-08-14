package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// modelsResp GET /v1/models 响应体。
type modelsResp struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// Models 获取供应商可用模型列表（能力识别 Q4A2 第一档：API 元数据）。
func (c *OpenAICompat) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	url := c.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构建模型列表请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKeys[0])

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, classifyStatus(resp.StatusCode, respBody)
	}

	var m modelsResp
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}

	// 基础元数据（完整能力字段需更丰富的元数据端点，此处先给 ID 级信息）
	models := make([]llm.ModelInfo, 0, len(m.Data))
	for _, d := range m.Data {
		models = append(models, llm.ModelInfo{
			ID:         d.ID,
			Provenance: "api",
		})
	}
	return models, nil
}

// classifyStatus 分类状态码（供 Models 用，返回 error）。
func classifyStatus(statusCode int, body []byte) error {
	switch {
	case statusCode == 401 || statusCode == 403:
		return fmt.Errorf("认证失败（%d）: %s", statusCode, string(body))
	case statusCode >= 500:
		return fmt.Errorf("服务器错误（%d）: %s", statusCode, string(body))
	default:
		return fmt.Errorf("未知错误（%d）: %s", statusCode, string(body))
	}
}
