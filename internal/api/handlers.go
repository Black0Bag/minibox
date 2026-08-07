package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/logging"
	"github.com/Black0Bag/minibox/internal/memory"
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
		"运行时长秒":  int(time.Since(s.startTime).Seconds()),
		"监听端口":   s.cfg.Server.Port,
		"数据目录":   s.cfg.DataDir,
		"日志级别":   logging.CurrentLevel(),
		"Go版本":   runtime.Version(),
		"内存占用MB": mem.Alloc / 1024 / 1024,
		"供应商数":   len(s.cfg.Providers),
	})
}

// handleVersion 版本信息（N-14：版本号单一来源）
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"版本": s.version,
		"协议": "v1",
	})
}

// handleHealthz 健康检查（watchdog 用，豁免限流）
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"状态": "ok",
		"版本": s.version,
		"时间": time.Now().Format("2006-01-02 15:04:05"),
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

// ============ 知识库 API（Phase 1）============

// kbCreateRequest 写入条目请求
type kbCreateRequest struct {
	Zone    string   `json:"zone"`
	Type    string   `json:"type"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Source  string   `json:"source"`
}

// handleKBCreate 写入知识条目（默认缓存区）
func (s *Server) handleKBCreate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "存储不可用")
		return
	}
	var req kbCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "content 不能为空")
		return
	}
	if req.Zone == "" {
		req.Zone = memory.ZoneCache
	}
	if req.Zone != memory.ZoneCache && req.Zone != memory.ZoneStore {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "zone 只能是 cache 或 store")
		return
	}
	id, err := s.store.CreateEntry(req.Zone, req.Type, req.Title, req.Content, req.Tags, req.Source)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	s.logger.Info("写入知识条目", "id", id, "zone", req.Zone)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "zone": req.Zone})
}

// handleKBGet 读取条目
func (s *Server) handleKBGet(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "id 必须是数字")
		return
	}
	e, err := s.store.GetEntry(id)
	if errors.Is(err, memory.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "条目不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleKBList 列表（?zone=&type=&limit=&offset=）
func (s *Server) handleKBList(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	zone := r.URL.Query().Get("zone")
	etype := r.URL.Query().Get("type")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	entries, total, err := s.store.ListEntries(zone, etype, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"条目": entries, "总数": total})
}

// handleKBSearch 全文搜索（?q=&zone=&limit=；配置了 embedding 时自动 RRF 融合向量）
func (s *Server) handleKBSearch(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	q := r.URL.Query().Get("q")
	zone := r.URL.Query().Get("zone")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var results []memory.SearchResult
	var err error
	if s.embedClient != nil {
		results, err = s.store.HybridSearch(q, zone, limit, s.embedClient)
	} else {
		results, err = s.store.Search(q, zone, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"结果": results, "总数": len(results)})
}

// handleKBSetVector 为条目写入向量（POST /知识库/{id}/向量 {text}）
func (s *Server) handleKBSetVector(w http.ResponseWriter, r *http.Request) {
	if s.store == nil || s.embedClient == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "向量未启用", "未配置 embedding")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "id 必须是数字")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "text 不能为空")
		return
	}
	// 确认条目存在
	if _, err := s.store.GetEntry(id); errors.Is(err, memory.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "条目不存在")
		return
	}
	vecs, err := s.embedClient.Embed(r.Context(), []string{req.Text})
	if err != nil {
		writeError(w, http.StatusBadGateway, "LLM_GATEWAY_ERROR", "embedding 服务异常", redact(err.Error()))
		return
	}
	if len(vecs) == 0 {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", "embedding 返回空")
		return
	}
	if err := s.store.SetVector(id, vecs[0]); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	s.logger.Info("写入条目向量", "id", id, "dim", len(vecs[0]))
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "状态": "向量已写入", "维度": len(vecs[0])})
}

// handleKBUpdate 更新条目
func (s *Server) handleKBUpdate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "id 必须是数字")
		return
	}
	var req kbCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	err = s.store.UpdateEntry(id, req.Title, req.Content, req.Tags)
	if errors.Is(err, memory.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "条目不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "状态": "已更新"})
}

// handleKBDelete 删除条目
func (s *Server) handleKBDelete(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "id 必须是数字")
		return
	}
	err = s.store.DeleteEntry(id)
	if errors.Is(err, memory.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "条目不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleKBClear 清空区域（?zone=）
func (s *Server) handleKBClear(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	zone := r.URL.Query().Get("zone")
	if zone == "" {
		zone = memory.ZoneCache
	}
	n, err := s.store.ClearZone(zone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"已清空": n, "zone": zone})
}

// ============ 编译管道 API（M2-2）============

// handleCompileSubmit 提交编译任务（POST /编译 {type,content}）
func (s *Server) handleCompileSubmit(w http.ResponseWriter, r *http.Request) {
	if s.compiler == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "编译管道未初始化", "未配置 LLM 供应商")
		return
	}
	var req struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "content 不能为空")
		return
	}
	id, err := s.compiler.Submit(req.Type, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "状态": "已提交"})
}

// handleCompileGet 查询编译任务状态
func (s *Server) handleCompileGet(w http.ResponseWriter, r *http.Request) {
	if s.compiler == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "编译管道未初始化", "")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "id 必须是数字")
		return
	}
	t, err := s.compiler.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleCompileList 编译任务列表
func (s *Server) handleCompileList(w http.ResponseWriter, r *http.Request) {
	if s.compiler == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "编译管道未初始化", "")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	tasks, err := s.compiler.List(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"任务": tasks, "总数": len(tasks)})
}

// ============ 蒸馏 API（M2-3）============

// handleDistillHit 正向证据：命中关键词上调概率
func (s *Server) handleDistillHit(w http.ResponseWriter, r *http.Request) {
	if s.distiller == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "蒸馏未初始化", "")
		return
	}
	var req struct {
		Keyword    string `json:"keyword"`
		Importance string `json:"importance"`
		Source     string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Keyword == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "keyword 不能为空")
		return
	}
	if err := s.distiller.Hit(req.Keyword, req.Importance, req.Source); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	s.logger.Info("蒸馏命中", "keyword", req.Keyword)
	writeJSON(w, http.StatusOK, map[string]any{"keyword": req.Keyword, "状态": "命中已记录"})
}

// handleDistillNegative 反向证据：用户否定下调概率
func (s *Server) handleDistillNegative(w http.ResponseWriter, r *http.Request) {
	if s.distiller == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "蒸馏未初始化", "")
		return
	}
	var req struct {
		Keyword string `json:"keyword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Keyword == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "keyword 不能为空")
		return
	}
	if err := s.distiller.Negative(req.Keyword); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keyword": req.Keyword, "状态": "反例已记录"})
}

// handleDistillList 列出蒸馏偏好
func (s *Server) handleDistillList(w http.ResponseWriter, r *http.Request) {
	if s.distiller == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "蒸馏未初始化", "")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	prefs, err := s.distiller.List(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"偏好": prefs, "总数": len(prefs)})
}

// handleDistillDelete 删除偏好
func (s *Server) handleDistillDelete(w http.ResponseWriter, r *http.Request) {
	if s.distiller == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "蒸馏未初始化", "")
		return
	}
	keyword := chi.URLParam(r, "keyword")
	ok, err := s.distiller.Delete(keyword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "偏好不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDistillDecay 触发老化衰减（T-07 TTL）
func (s *Server) handleDistillDecay(w http.ResponseWriter, r *http.Request) {
	if s.distiller == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "蒸馏未初始化", "")
		return
	}
	n, err := s.distiller.Decay()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"已衰减": n})
}

// ============ 快照 API（M2-4）============

// snapshotDir 返回快照目录
func (s *Server) snapshotDir() string {
	return filepath.Join(s.cfg.DataDir, "snapshots")
}

// handleSnapshotCreate 创建一致性快照（VACUUM INTO）
func (s *Server) handleSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	name, err := s.store.Snapshot(s.snapshotDir())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	s.logger.Info("创建快照", "name", name)
	writeJSON(w, http.StatusCreated, map[string]any{"快照": name, "状态": "已创建"})
}

// handleSnapshotList 列出快照
func (s *Server) handleSnapshotList(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	names, err := s.store.ListSnapshots(s.snapshotDir())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"快照列表": names, "总数": len(names)})
}

// handleSnapshotRestore 从快照恢复（POST /快照/恢复 {file}）
func (s *Server) handleSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	var req struct {
		File string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.File == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "file 不能为空")
		return
	}
	path := filepath.Join(s.snapshotDir(), filepath.Base(req.File))
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "快照文件不存在")
		return
	}
	if err := s.store.Restore(path); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	s.logger.Info("从快照恢复", "file", req.File)
	writeJSON(w, http.StatusOK, map[string]any{"状态": "已恢复", "快照": req.File})
}
