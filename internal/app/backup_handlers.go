package app

import "net/http"

// --- 备份域 ---

// handleBackupList 列出备份。
func (a *App) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	list, err := a.backup.List()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "backup_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.backups.list", map[string]any{"backups": list})
}

// handleBackupSnapshot 创建快照。
func (a *App) handleBackupSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "backup_unavailable", "备份未就绪")
		return
	}
	path, err := a.backup.Snapshot()
	if err != nil {
		a.respondErr(w, r, http.StatusInternalServerError, "backup_error", err.Error())
		return
	}
	a.respondOK(w, r, "api.backups.export", map[string]string{"path": path})
}
