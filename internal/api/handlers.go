package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/logging"
)

// 敏感信息脱敏（N-05：API Key 等 token 统一脱敏，避免明文泄露）
var sensitiveRe = regexp.MustCompile(`(sk-)[A-Za-z0-9_\-]{8,}`)

// redact 将字符串中的 sk-* token 脱敏
func redact(s string) string {
	return sensitiveRe.ReplaceAllString(s, "sk-***")
}

// maskKey 脱敏 API Key：显示前 2 后 4（N-05）
func maskKey(key string) string {
	if len(key) <= 6 {
		return "***"
	}
	return key[:2] + "****" + key[len(key)-4:]
}

// Problem RFC 7807 错误响应（N-09）
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 输出 RFC 7807 错误
func writeError(w http.ResponseWriter, status int, code, title, detail string) {
	writeJSON(w, status, Problem{
		Type:   "https://api.minibox.local/errors/" + strings.ToLower(code),
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code,
	})
}

// handleStatus 系统状态（含健康检查信息）
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	writeJSON(w, http.StatusOK, map[string]any{
		"状态":     "运行中",
		"版本":     s.version,
		"运行时长秒": int(time.Since(s.startTime).Seconds()),
		"监听端口":  s.cfg.Server.Port,
		"数据目录":  s.cfg.DataDir,
		"日志级别":  logging.CurrentLevel(),
		"Go版本":  runtime.Version(),
		"内存占用MB": mem.Alloc / 1024 / 1024,
		"供应商数":  len(s.cfg.Providers),
	})
}

// handleVersion 版本信息（N-14：版本号单一来源）
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"版本":  s.version,
		"协议":  "v1",
	})
}

// handleHealthz 健康检查（watchdog 用，豁免限流）
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"状态":  "ok",
		"版本":  s.version,
		"时间":  time.Now().Format("2006-01-02 15:04:05"),
	})
}

// handleLogs 日志查询（T-10：GET /api/v1/logs?level=&limit=&since=）
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	if level == "" {
		level = "info"
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
			since = t
		}
	}
	entries, err := logging.Query(level, limit, since)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误", err.Error())
		return
	}
	// 脱敏后返回
	type out struct {
		Time    string         `json:"time"`
		Level   string         `json:"level"`
		Message string         `json:"message"`
		Attrs   map[string]any `json:"attrs,omitempty"`
	}
	result := make([]out, 0, len(entries))
	for _, e := range entries {
		attrs := make(map[string]any, len(e.Attrs))
		for k, v := range e.Attrs {
			attrs[k] = redact(fmt.Sprintf("%v", v))
		}
		result = append(result, out{
			Time:    e.Time.Format("2006-01-02 15:04:05"),
			Level:   e.Level,
			Message: redact(e.Message),
			Attrs:   attrs,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"日志": result, "总数": len(result)})
}

// providerView 供应商对外视图（key 脱敏）
type providerView struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

func toView(p config.Provider, reveal bool) providerView {
	v := providerView{Name: p.Name, BaseURL: p.BaseURL, Model: p.Model, Enabled: p.Enabled}
	if reveal {
		v.APIKey = p.APIKey
	} else {
		v.APIKey = maskKey(p.APIKey)
	}
	return v
}

// handleListProviders 列出供应商（key 默认脱敏；?reveal=true 返回明文，N-05）
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	reveal := r.URL.Query().Get("reveal") == "true"
	views := make([]providerView, 0, len(s.cfg.Providers))
	for _, p := range s.cfg.Providers {
		views = append(views, toView(p, reveal))
	}
	writeJSON(w, http.StatusOK, map[string]any{"供应商": views})
}

func (s *Server) handleAddProvider(w http.ResponseWriter, r *http.Request) {
	var p config.Provider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if p.Name == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "供应商 name 不能为空")
		return
	}
	if _, ok := s.cfg.FindProvider(p.Name); ok {
		writeError(w, http.StatusConflict, "CONFLICT", "冲突", fmt.Sprintf("供应商 %s 已存在", p.Name))
		return
	}
	s.cfg.Providers = append(s.cfg.Providers, p)
	s.logger.Info("新增供应商", "name", p.Name)
	writeJSON(w, http.StatusCreated, toView(p, false))
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	reveal := r.URL.Query().Get("reveal") == "true"
	p, ok := s.cfg.FindProvider(name)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", fmt.Sprintf("供应商 %s 不存在", name))
		return
	}
	writeJSON(w, http.StatusOK, toView(*p, reveal))
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, ok := s.cfg.FindProvider(name)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", fmt.Sprintf("供应商 %s 不存在", name))
		return
	}
	var update config.Provider
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	// 合并更新（空字段保留原值）
	if update.BaseURL != "" {
		p.BaseURL = update.BaseURL
	}
	if update.APIKey != "" {
		p.APIKey = update.APIKey
	}
	if update.Model != "" {
		p.Model = update.Model
	}
	p.Enabled = update.Enabled
	s.logger.Info("更新供应商", "name", name)
	writeJSON(w, http.StatusOK, toView(*p, false))
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	idx := -1
	for i := range s.cfg.Providers {
		if s.cfg.Providers[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", fmt.Sprintf("供应商 %s 不存在", name))
		return
	}
	s.cfg.Providers = append(s.cfg.Providers[:idx], s.cfg.Providers[idx+1:]...)
	s.logger.Info("删除供应商", "name", name)
	w.WriteHeader(http.StatusNoContent)
}
