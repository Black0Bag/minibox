// handleMonitor 端点实现：性能监控 REST API（Phase 3.5）。
package app

import (
	"net/http"
	"sync"

	"github.com/Black0Bag/minibox/internal/monitor"
)

// handleMonitorMetrics GET /api/v1/monitor/metrics
// 采集并返回当前系统性能指标快照（CPU/内存/磁盘/进程）。
func (a *App) handleMonitorMetrics(w http.ResponseWriter, r *http.Request) {
	if a.perfCollector == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "monitor_unavailable", "性能监控未就绪")
		return
	}
	a.respondOK(w, r, "api.monitor.metrics", a.perfCollector.Collect())
}

// handleMonitorHistory GET /api/v1/monitor/history?limit=60
// 返回最近的性能指标历史（内存环形缓冲，默认保留最近 60 个采样点）。
func (a *App) handleMonitorHistory(w http.ResponseWriter, r *http.Request) {
	if a.perfHistory == nil {
		a.respondErr(w, r, http.StatusServiceUnavailable, "monitor_unavailable", "性能监控未就绪")
		return
	}
	a.respondOK(w, r, "api.monitor.history", a.perfHistory.Snapshot())
}

// historyRing 内存环形缓冲（最近 N 条指标快照）。
// 并发约束：Push 由 App.Run 的 30s 采样 goroutine 调用，Snapshot 由 HTTP handler
// 调用，两者并发访问同一 data 切片，必须加锁（否则为 data race）。
type historyRing struct {
	mu    sync.Mutex
	limit int
	data  []monitor.Metrics
}

// newHistoryRing 创建环形缓冲。
func newHistoryRing(limit int) *historyRing {
	if limit <= 0 {
		limit = 60
	}
	return &historyRing{limit: limit}
}

// Push 追加一条（超出容量淘汰最旧）。
func (h *historyRing) Push(m monitor.Metrics) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.data = append(h.data, m)
	if len(h.data) > h.limit {
		h.data = h.data[len(h.data)-h.limit:]
	}
}

// Snapshot 返回当前缓冲副本（旧→新）。
// 始终返回非 nil 切片：JSON 序列化为 []，避免前端拿到 null 需要额外判空。
func (h *historyRing) Snapshot() []monitor.Metrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]monitor.Metrics, len(h.data))
	copy(out, h.data)
	return out
}
