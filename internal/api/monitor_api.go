package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// ============ 性能监控 API（M5）============

// handleMonitor 系统性能指标（GET /监控）
func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	m, err := s.monitor.Collect()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	// 顺便评估降级，返回当前级别
	lvl := s.degrader.Evaluate(time.Now())
	writeJSON(w, http.StatusOK, map[string]any{
		"指标":   m,
		"降级级别": lvl.Name(),
	})
}

// handleDegrade 当前降级状态（GET /降级）
func (s *Server) handleDegrade(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.degrader.Snapshot())
}

// ============ 设备代理 Hub API（M5）============

// handleDevicePairingCode 生成配对码（POST /设备/配对码 {name}）
func (s *Server) handleDevicePairingCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if req.Name == "" {
		req.Name = "未命名设备"
	}
	code, err := s.devices.NewPairingCode(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	s.logger.Info("生成设备配对码", "name", req.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"配对码": code, "提示": "请向设备提供此配对码完成注册"})
}

// handleDeviceRegister 设备配对注册（POST /设备 {code,id,type,capabilities}）
func (s *Server) handleDeviceRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code         string   `json:"code"`
		ID           string   `json:"id"`
		Type         string   `json:"type"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求参数错误", "JSON 解析失败")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "校验失败", "code 不能为空")
		return
	}
	dev, err := s.devices.Register(req.Code, req.ID, req.Type, req.Capabilities)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_ERROR", "配对失败", err.Error())
		return
	}
	s.logger.Info("设备注册", "id", dev.ID, "type", dev.Type)
	writeJSON(w, http.StatusCreated, dev)
}

// handleDeviceList 设备列表
func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	devs, err := s.devices.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"设备": devs, "总数": len(devs)})
}

// handleDeviceGet 设备详情
func (s *Server) handleDeviceGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	dev, err := s.devices.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "设备不存在")
		return
	}
	_, _ = s.devices.CheckOnline(id) // 更新在线状态
	writeJSON(w, http.StatusOK, dev)
}

// handleDeviceHeartbeat 设备心跳（POST /设备/{id}/心跳）
func (s *Server) handleDeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.devices.Heartbeat(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "设备不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"设备": id, "状态": "心跳已更新"})
}

// handleDeviceOffline 标记离线（POST /设备/{id}/离线）
func (s *Server) handleDeviceOffline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.devices.MarkOffline(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "设备不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"设备": id, "状态": "已标记离线"})
}

// handleDeviceUnregister 注销设备
func (s *Server) handleDeviceUnregister(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.devices.Unregister(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", "设备不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeviceAudit 设备审计日志（GET /设备/审计?limit=）
func (s *Server) handleDeviceAudit(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	log, err := s.devices.AuditLog(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"审计": log, "总数": len(log)})
}
