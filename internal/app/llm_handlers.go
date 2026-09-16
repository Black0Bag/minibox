package app

import (
	"encoding/json"
	"net/http"

	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// --- LLM 域 ---

// handleLLMProviders 列出 LLM 供应商。
func (a *App) handleLLMProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]string, 0, len(a.cfg.LLM.Providers))
	for _, p := range a.cfg.LLM.Providers {
		providers = append(providers, p.Name)
	}
	a.respondOK(w, r, "api.llm.providers", map[string]any{
		"default_provider": a.cfg.LLM.DefaultProvider,
		"providers":        providers,
	})
}

// handleLLMModels 列出模型（能力识别）。
func (a *App) handleLLMModels(w http.ResponseWriter, r *http.Request) {
	if a.llm == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "llm_unavailable", "LLM 未就绪")
		return
	}
	models, err := a.llm.Models(r.Context())
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "models_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.llm.models", map[string]any{"models": models})
}

// handleLLMModelsRefresh 刷新模型列表。
func (a *App) handleLLMModelsRefresh(w http.ResponseWriter, r *http.Request) {
	a.handleLLMModels(w, r)
}

// handleLLMFeatureModels 获取功能级模型配置（B6）。
func (a *App) handleLLMFeatureModels(w http.ResponseWriter, r *http.Request) {
	models := a.FeatureModels()
	if models == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "feature_models_unavailable", "FeatureRouter 未就绪")
		return
	}
	a.respondOK(w, r, "api.llm.feature_models", models)
}

// handleLLMUpdateFeatureModels 更新功能级模型配置（B6）。
func (a *App) handleLLMUpdateFeatureModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Configs []llm.FeatureConfig `json:"configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}

	models := &llm.FeatureModels{Configs: make(map[llm.Feature]llm.FeatureConfig)}
	for _, fc := range req.Configs {
		if err := fc.Validate(); err != nil {
			a.respondErr(w, r, http.StatusBadRequest, "invalid_feature_config", err.Error())
			return
		}
		models.Set(fc)
	}

	a.UpdateFeatureModels(models)
	a.respondOK(w, r, "api.llm.feature_models.updated", models)
}
