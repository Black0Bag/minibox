package device

import (
	"sync"
	"time"
)

// AuditLogger 审计日志（D-15：所有命令/结果落本地双份日志）。
type AuditLogger struct {
	mu   sync.RWMutex
	logs []*Command
}

// NewAuditLogger 创建审计日志。
func NewAuditLogger() *AuditLogger {
	return &AuditLogger{logs: make([]*Command, 0, 100)}
}

// Log 记录命令审计。
func (al *AuditLogger) Log(cmd *Command) {
	al.mu.Lock()
	al.logs = append(al.logs, cmd)
	al.mu.Unlock()
}

// List 列出审计日志。
func (al *AuditLogger) List() []*Command {
	al.mu.RLock()
	defer al.mu.RUnlock()
	out := make([]*Command, len(al.logs))
	copy(out, al.logs)
	return out
}

// CleanOlderThan 清理旧日志。
func (al *AuditLogger) CleanOlderThan(d time.Duration) {
	cutoff := time.Now().Add(-d)
	al.mu.Lock()
	defer al.mu.Unlock()
	keep := 0
	for _, cmd := range al.logs {
		if cmd.CreatedAt.After(cutoff) {
			al.logs[keep] = cmd
			keep++
		}
	}
	al.logs = al.logs[:keep]
}