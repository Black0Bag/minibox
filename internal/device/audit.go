package device

import (
	"encoding/json"
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

// List 列出审计日志（深拷贝，防止调用方篡改内部记录）。
func (al *AuditLogger) List() []*Command {
	al.mu.RLock()
	defer al.mu.RUnlock()
	out := make([]*Command, len(al.logs))
	for i, cmd := range al.logs {
		cp := *cmd
		if cmd.Params != nil {
			cp.Params = append(json.RawMessage(nil), cmd.Params...)
		}
		if cmd.Result != nil {
			r := *cmd.Result
			cp.Result = &r
		}
		out[i] = &cp
	}
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