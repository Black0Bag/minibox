// Package fsutil 提供 logme 足迹系统（B21：代码级 enforce，只追加不删改）。
// 设计：PRD B21 + openai/codex #28224 教训（21 天 37TB 烧穿 SSD）。
// 原则：扁平文本文件，按日期分文件，只追加写，mtime 过期检测，不进主数据库。
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Logme 足迹写入器（只追加不删改，代码级 enforce）。
type Logme struct {
	mu        sync.Mutex
	dir       string
	retention time.Duration
}

// NewLogme 创建足迹写入器。
// dir 为日志文件夹（如 data/logme），retention 为过期时间（0 表示不清理）。
func NewLogme(dir string, retention time.Duration) *Logme {
	return &Logme{dir: dir, retention: retention}
}

// Log 记录一条足迹（只追加，不删改）。
// action: 操作名（如 "agent.step"、"tool.invoke"、"kb.search"）
// traceID: 链路追踪 ID
// detail: 附加信息（JSON 字符串或纯文本）
func (l *Logme) Log(action, traceID, detail string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 按日期分文件：data/logme/2026-08-16.log
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(l.dir, today+".log")

	if err := os.MkdirAll(l.dir, 0o750); err != nil {
		return fmt.Errorf("创建 logme 目录失败: %w", err)
	}

	// 只追加，0600 权限（N-05）。
	// #nosec G304 -- path 由 NewLogme(dir) 配置注入 + 日期文件拼接，非不可信输入
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开 logme 文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	// 格式：时间戳 action trace_id detail
	line := fmt.Sprintf("[%s] %s %s %s\n",
		time.Now().UTC().Format("2006-01-02 15:04:05.000Z07:00"),
		action, traceID, detail)
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("写入 logme 失败: %w", err)
	}
	return nil
}

// LogJSON 记录一条足迹（detail 为 JSON 字符串）。
func (l *Logme) LogJSON(action, traceID string, detail any) error {
	d := fmt.Sprint(detail)
	return l.Log(action, traceID, d)
}

// CleanExpired 清理过期日志文件（按 mtime 过期检测）。
func (l *Logme) CleanExpired() (int, error) {
	if l.retention <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-l.retention)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(l.dir, e.Name())
			if err := os.Remove(path); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}
