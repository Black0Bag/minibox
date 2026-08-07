package api

import (
	"context"
	"net/http"
)

// ============ 备份 API（M7）============

// handleBackupRun 手动触发备份（POST /备份）
func (s *Server) handleBackupRun(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "无法备份")
		return
	}
	name, err := s.backups.Run(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", redact(err.Error()))
		return
	}
	s.logger.Info("手动备份", "file", name)
	writeJSON(w, http.StatusCreated, map[string]any{"快照": name, "状态": "已备份"})
}

// handleBackupList 备份列表
func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	list, err := s.backups.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"快照列表": list, "总数": len(list)})
}

// handleBackupRecords 备份记录（含大小/时间）
func (s *Server) handleBackupRecords(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "知识库未初始化", "")
		return
	}
	recs, err := s.backups.Records()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "内部错误", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"记录": recs, "总数": len(recs)})
}

// ============ 自升级 API（M7）============

// handleUpgradeStatus 升级/健康状态（GET /升级）
func (s *Server) handleUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.upgrader.Status())
}

// handleUpgradeCheck 健康检查（POST /升级/检查）
func (s *Server) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	ok, err := s.upgrader.CheckHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "HEALTH_CHECK_ERROR", "健康检查失败", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"健康": ok})
}

// handleUpgradeApply 应用升级（POST /升级/应用）——保守两阶段
func (s *Server) handleUpgradeApply(w http.ResponseWriter, r *http.Request) {
	if err := s.upgrader.ApplyUpgrade(context.Background()); err != nil {
		writeError(w, http.StatusBadGateway, "UPGRADE_ERROR", "升级失败", redact(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, s.upgrader.LastUpgrade())
}
