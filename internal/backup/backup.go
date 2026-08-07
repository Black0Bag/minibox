package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Snapper 快照生成接口（由 memory.Store 实现：VACUUM INTO 一致性快照）
type Snapper interface {
	Snapshot(dir string) (string, error)
}

// Record 备份记录
type Record struct {
	File      string `json:"file"`
	CreatedAt string `json:"created_at"`
	SizeBytes int64  `json:"size_bytes"`
}

// Manager 备份管理器（Phase 5：定期 VACUUM INTO 快照 + 保留最近 N 份 + 推送记录）
type Manager struct {
	mu      sync.Mutex
	dir     string
	snap    Snapper
	keep    int
	records []Record
	recordsPath string
	interval time.Duration
}

// NewManager 创建备份管理器（dir=快照目录，keep=保留份数，interval=间隔）
func NewManager(dir string, snap Snapper, keep int, interval time.Duration) *Manager {
	m := &Manager{
		dir:         dir,
		snap:        snap,
		keep:        keep,
		interval:    interval,
		recordsPath: filepath.Join(dir, "backup.json"),
	}
	if keep <= 0 {
		m.keep = 5
	}
	if interval <= 0 {
		m.interval = time.Hour
	}
	_ = m.loadRecords()
	return m
}

// Keep 保留份数
func (m *Manager) Keep() int { return m.keep }

// Dir 快照目录
func (m *Manager) Dir() string { return m.dir }

// Run 执行一次备份：生成快照 + 清理过期 + 记录
func (m *Manager) Run(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录: %w", err)
	}
	name, err := m.snap.Snapshot(m.dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(m.dir, name)
	info, _ := os.Stat(path)
	var size int64
	if info != nil {
		size = info.Size()
	}
	m.mu.Lock()
	m.records = append([]Record{{File: name, CreatedAt: time.Now().Format("2006-01-02 15:04:05"), SizeBytes: size}}, m.records...)
	_ = m.saveRecordsLocked()
	m.mu.Unlock()
	// 清理超出保留份数的旧快照
	if err := m.prune(); err != nil {
		return name, err
	}
	return name, nil
}

// prune 保留最近 keep 份，删除更早的
func (m *Manager) prune() error {
	list, err := m.List()
	if err != nil {
		return err
	}
	// list 已按时间倒序
	for i := m.keep; i < len(list); i++ {
		p := filepath.Join(m.dir, list[i])
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除旧快照 %s: %w", list[i], err)
		}
	}
	return nil
}

// List 列出快照（按名称倒序，即时间倒序）
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取备份目录: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".db" {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// Records 备份记录（含大小）
func (m *Manager) Records() ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, len(m.records))
	copy(out, m.records)
	return out, nil
}

// Start 后台定期备份循环
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = m.Run(ctx)
			}
		}
	}()
}

func (m *Manager) saveRecordsLocked() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(m.records, "", "  ")
	return os.WriteFile(m.recordsPath, data, 0o600)
}

func (m *Manager) loadRecords() error {
	data, err := os.ReadFile(m.recordsPath)
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(data, &m.records)
	return nil
}
