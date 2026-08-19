// Package llm 提供多供应商 LLM 路由 + 功能级模型独立配置（B6）。
//
// FeatureRouter 是 Router 的包装层，职责：
//  1. 根据 llm.Request.Feature 查找对应的功能级模型配置
//  2. 按配置的 provider+model 解析到具体 ProviderEntry
//  3. 未配置时回退到 Router 的默认策略（round-robin/fallback）
//
// 依据：B6 每个需调用 LLM 的功能都可独立配置模型。
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// SystemConfigStore 持久化接口（由 storage 包实现，避免循环依赖）。
type SystemConfigStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

// FeatureRouter 功能级模型路由（B6 装饰器）。
// 包装 Router，根据 req.Feature 选择模型。
type FeatureRouter struct {
	inner  *Router                     // 底层路由（含所有 ProviderEntry）
	models *llm.FeatureModels          // 功能→模型映射（可运行时更新）
	store  SystemConfigStore           // 持久化（非 nil 时 PATCH 自动写 DB）
	logger *slog.Logger
}

// NewFeatureRouter 创建功能级模型路由。
// inner 是已装配好的底层 Router，models 为初始功能配置。
func NewFeatureRouter(inner *Router, models *llm.FeatureModels, logger *slog.Logger) *FeatureRouter {
	if models == nil {
		models = &llm.FeatureModels{Configs: make(map[llm.Feature]llm.FeatureConfig)}
	}
	return &FeatureRouter{
		inner:  inner,
		models: models,
		logger: logger,
	}
}

// SetStore 设置持久化存储（可选，未设置时不持久化）。
func (fr *FeatureRouter) SetStore(store SystemConfigStore) {
	fr.store = store
}

// LoadFromStore 从持久化存储加载配置（启动时调用）。
func (fr *FeatureRouter) LoadFromStore() error {
	if fr.store == nil {
		return nil
	}
	raw, err := fr.store.Get("feature.models")
	if err != nil || raw == "" {
		return err
	}
	var models llm.FeatureModels
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return fmt.Errorf("解析功能级模型配置失败: %w", err)
	}
	if models.Configs == nil {
		models.Configs = make(map[llm.Feature]llm.FeatureConfig)
	}
	fr.models = &models
	return nil
}

// UpdateModels 运行时更新功能级模型配置（由 REST 端点调用）。
// 有持久化存储时自动写入 DB。
func (fr *FeatureRouter) UpdateModels(models *llm.FeatureModels) {
	fr.models = models
	if fr.store != nil {
		b, err := json.Marshal(models)
		if err != nil {
			fr.logger.Error("功能级模型配置序列化失败", "err", err)
			return
		}
		if err := fr.store.Set("feature.models", string(b)); err != nil {
			fr.logger.Error("功能级模型配置持久化失败", "err", err)
		}
	}
}

// FeatureModels 返回当前功能级模型配置快照。
func (fr *FeatureRouter) FeatureModels() *llm.FeatureModels {
	return fr.models
}

// Complete 实现 llm.Provider.Complete，带功能级模型路由。
func (fr *FeatureRouter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	resolved, err := fr.resolve(req)
	if err != nil {
		return nil, err
	}
	return fr.inner.Complete(ctx, resolved)
}

// Stream 实现 llm.Provider.Stream，带功能级模型路由。
func (fr *FeatureRouter) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	resolved, err := fr.resolve(req)
	if err != nil {
		return nil, err
	}
	return fr.inner.Stream(ctx, resolved)
}

// Name 返回路由器标识（llm.Provider 接口要求）。
func (fr *FeatureRouter) Name() string {
	return "feature_router"
}

// Models 获取模型能力清单（委托给底层 Router）。
func (fr *FeatureRouter) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	return fr.inner.Models(ctx)
}

// resolve 根据 req.Feature 和功能级配置解析出最终请求。
// 策略：
//   1. req.Model 已设置 → 直接使用（调用方显式指定优先级最高）
//   2. 功能级配置存在且指定 Provider → 设置 req.Model 并标记 Provider 偏好
//   3. 功能级配置存在但未指定 Provider → 仅设置 req.Model
//   4. 均未配置 → 保持 req.Model=""，由 Router 按默认策略路由
func (fr *FeatureRouter) resolve(req llm.Request) (llm.Request, error) {
	if req.Model != "" {
		return req, nil
	}

	if req.Feature != "" {
		cfg := fr.models.Get(req.Feature)
		if cfg.Model != "" {
			fr.logger.Debug("功能级模型配置生效",
				"feature", req.Feature,
				"provider", cfg.Provider,
				"model", cfg.Model,
			)
			req.Model = cfg.Model
			return req, nil
		}
		if err := cfg.Validate(); err == nil {
			fr.logger.Debug("功能未配置独立模型，使用默认",
				"feature", req.Feature,
			)
		}
	}

	return req, nil
}

