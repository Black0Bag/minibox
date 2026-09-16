package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/memory"
	"github.com/Black0Bag/minibox/internal/infrastructure/backup"
	"github.com/go-chi/chi/v5"
)

// --- 知识库域 ---

// kbSearchReq 检索请求。
type kbSearchReq struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

// handleKBSearch 检索知识库（三级降级）。
func (a *App) handleKBSearch(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "memory_unavailable", "知识库未就绪")
		return
	}
	var req kbSearchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "query 必填")
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	query := memory.SearchQuery{Text: req.Query, TopK: req.TopK, Tier: memory.TierStore}
	// 查询向量：embedder 可用时走 hybrid（vector+FTS5，asymmetric 模型须 query 模式）
	if a.embedder != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if vec, err := a.embedder.EmbedQuery(ctx, req.Query); err == nil && len(vec) > 0 {
			query.QueryVector = vec
		}
	}
	hits, err := a.memory.Search(r.Context(), query)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "search_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.search", map[string]any{"hits": hits})
}

// handleKBList 分页列出知识库。
func (a *App) handleKBList(w http.ResponseWriter, r *http.Request) {
	if a.memory == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "memory_unavailable", "知识库未就绪")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	entries, err := a.memory.List(r.Context(), memory.TierStore, offset, limit)
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "list_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.list", map[string]any{"entries": entries})
}

// handleKBGet 获取单条。
func (a *App) handleKBGet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	e, err := a.memory.Get(r.Context(), id, memory.TierStore)
	if err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "条目不存在")
		return
	}
	a.respondOK(w, r, "api.kb.store.get", e)
}

// handleKBCreate 写入条目。
func (a *App) handleKBCreate(w http.ResponseWriter, r *http.Request) {
	var e memory.Entry
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil || e.Content == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "content 必填")
		return
	}
	if err := a.memory.Upsert(r.Context(), e); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "upsert_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.create", map[string]bool{"ok": true})
}

// handleKBUpdate 更新条目。
func (a *App) handleKBUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var e memory.Entry
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil || id <= 0 {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	e.ID = id
	if err := a.memory.Upsert(r.Context(), e); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "upsert_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.update", map[string]bool{"ok": true})
}

// handleKBDelete 删除条目。
func (a *App) handleKBDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "id 无效")
		return
	}
	if err := a.memory.Delete(r.Context(), id, memory.TierStore); err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "delete_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.store.delete", map[string]bool{"ok": true})
}

// handleKBCompile 提交编译作业。
func (a *App) handleKBCompile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Source == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "source 必填")
		return
	}
	job, err := a.compiler.Compile(r.Context(), req.Source, memory.CompileOptions{})
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "compile_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.compile.create", job)
}

// handleKBCompileJob 查询编译作业状态。
func (a *App) handleKBCompileJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	job, err := a.compiler.GetJob(r.Context(), id)
	if err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "作业不存在: "+id)
		return
	}
	a.respondOK(w, r, "api.kb.compile.get", job)
}

// handleKBSnapshotsList 列出知识库快照。
func (a *App) handleKBSnapshotsList(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	list, err := a.backup.List()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "backup_list_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.snapshots.list", map[string]any{
		"count":     len(list),
		"snapshots": list,
	})
}

// handleKBSnapshotCreate 创建知识库快照（VACUUM INTO）。
func (a *App) handleKBSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	path, err := a.backup.Snapshot()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "snapshot_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.snapshots.create", map[string]string{
		"path":   path,
		"status": "ok",
	})
}

// handleKBRollback 从快照回滚知识库。
// 注意：回滚需重启服务使新数据库生效，当前返回 202 Accepted。
func (a *App) handleKBRollback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Snapshot string `json:"snapshot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.respondErr(w, r, http.StatusBadRequest, "invalid_json", "请求体解析失败: "+err.Error())
		return
	}
	if req.Snapshot == "" {
		a.respondErr(w, r, http.StatusBadRequest, "missing_snapshot", "snapshot 字段必填")
		return
	}
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	if err := a.backup.Restore(req.Snapshot); err != nil {
		// 非法快照名属于客户端输入错误 → 400；其余（IO/权限）→ 500
		if errors.Is(err, backup.ErrInvalidSnapshotName) {
			a.respondErr(w, r, http.StatusBadRequest, "invalid_snapshot", err.Error())
			return
		}
		a.respondErr(w, r, http.StatusInternalServerError, "rollback_failed", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.rollback", map[string]string{
		"snapshot": req.Snapshot,
		"status":   "ok",
		"note":     "数据库已替换，建议重启服务使新数据库生效",
	})
}

// handleKBDistill 执行蒸馏。
func (a *App) handleKBDistill(w http.ResponseWriter, r *http.Request) {
	res, err := a.distiller.Distill(r.Context(), memory.DistillOptions{})
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "distill_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.kb.distill", res)
}
