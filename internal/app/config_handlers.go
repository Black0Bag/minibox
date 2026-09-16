package app

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Black0Bag/minibox/internal/config"
)

// --- 配置域 ---
// handleServerStatus 服务器状态。
func (a *App) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.server.status", map[string]any{
		"uptime": time.Since(a.startTime).Seconds(),
		"ok":     true,
	})
}

// handleConfigUpdate 更新配置（运行时热重载可更新项）。
func (a *App) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Logging *config.LoggingConfig `json:"logging,omitempty"`
		LLM     *struct {
			DefaultModel string `json:"default_model,omitempty"`
		} `json:"llm,omitempty"`
		Server *struct {
			Port int `json:"port,omitempty"`
		} `json:"server,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}

	// 深拷贝当前配置，只更新允许的字段
	cfg := a.cfg
	if req.Logging != nil {
		if req.Logging.Level != "" {
			cfg.Logging.Level = req.Logging.Level
		}
		if req.Logging.Format != "" {
			cfg.Logging.Format = req.Logging.Format
		}
	}
	if req.LLM != nil && req.LLM.DefaultModel != "" {
		cfg.LLM.DefaultModel = req.LLM.DefaultModel
	}
	if req.Server != nil && req.Server.Port > 0 {
		cfg.Server.Port = req.Server.Port
	}

	a.UpdateConfig(cfg)
	a.respondOK(w, r, "api.config.updated", map[string]any{
		"server_port":   cfg.Server.Port,
		"default_model": cfg.LLM.DefaultModel,
		"log_level":     cfg.Logging.Level,
	})
}
