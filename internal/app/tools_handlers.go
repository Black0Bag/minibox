package app

import (
	"encoding/json"
	"net/http"

	"github.com/Black0Bag/minibox/internal/domain/permission"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	infratools "github.com/Black0Bag/minibox/internal/infrastructure/tools"
)

// --- 工具域+权限域 ---
func (a *App) handleToolList(w http.ResponseWriter, r *http.Request) {
	reg := a.toolReg()
	list := reg.List()
	type toolView struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Metadata    tools.Metadata  `json:"metadata"`
		JSONSchema  json.RawMessage `json:"json_schema"`
	}
	views := make([]toolView, 0, len(list))
	for _, t := range list {
		views = append(views, toolView{
			Name:        t.Name(),
			Description: t.Description(),
			Metadata:    t.Metadata(),
			JSONSchema:  t.JSONSchema(),
		})
	}
	a.respondOK(w, r, "api.tools.list", map[string]any{
		"count": len(views),
		"tools": views,
	})
}

// handlePermissionsGet 获取当前权限配置。
func (a *App) handlePermissionsGet(w http.ResponseWriter, r *http.Request) {
	a.respondOK(w, r, "api.permissions.get", map[string]any{
		"mode": a.PermissionMode(),
		"modes": []permission.Mode{
			permission.ModeYolo,
			permission.ModeAcceptEdits,
			permission.ModeAsk,
			permission.ModePlan,
		},
	})
}

// handlePermissionsMode 更新权限模式（运行时动态切换）。
func (a *App) handlePermissionsMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode permission.Mode `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}
	switch req.Mode {
	case permission.ModeYolo, permission.ModeAcceptEdits, permission.ModeAsk, permission.ModePlan:
		a.SetPermissionMode(req.Mode)
		a.respondOK(w, r, "api.permissions.mode.updated", map[string]permission.Mode{
			"mode": req.Mode,
		})
	default:
		a.respondErr(w, r, http.StatusBadRequest, "invalid_mode", "无效权限模式: "+string(req.Mode))
	}
}

// handleToolAcquire 触发 B8 工具自动获取。
// 根据 spec 下载外部工具二进制（SHA-256 校验 + 隔离目录）。
func (a *App) handleToolAcquire(w http.ResponseWriter, r *http.Request) {
	var spec infratools.ToolSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}
	if spec.Name == "" || spec.URL == "" || spec.SHA256 == "" {
		a.respondErr(w, r, http.StatusBadRequest, "missing_fields", "name/url/sha256 必填")
		return
	}
	res, err := a.AcquireTool(r.Context(), spec)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "acquire_failed", err.Error())
		return
	}
	// B8 闭环：下载成功后注册可执行包装，LLM 后续可直接调用该工具
	a.registerExecTool(spec, res.Path)
	a.respondOK(w, r, "api.tools.acquire", map[string]any{
		"path":       res.Path,
		"installed":  res.Installed,
		"registered": true,
	})
}
