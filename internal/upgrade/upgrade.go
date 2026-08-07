package upgrade

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manager 自升级 + watchdog 管理器（Phase 5，T-08 五位一体）
// 保守两阶段升级：先检查健康，再应用更新；失败自动恢复。
type Manager struct {
	mu         sync.Mutex
	healthURL  string
	downloadDir string
	lastCheck  time.Time
	lastErr    string
	lastUpgrade UpgradeRecord

	upgradeHook func() error
	restartHook func()
	httpClient  *http.Client
}

// UpgradeRecord 升级记录
type UpgradeRecord struct {
	Upgraded  bool   `json:"upgraded"`
	Version   string `json:"version"`
	At        string `json:"at"`
	Installed string `json:"installed,omitempty"`
}

// NewManager 创建升级管理器（healthURL 为本服务健康检查地址）
func NewManager(healthURL string, _ int) *Manager {
	m := &Manager{
		healthURL:   healthURL,
		downloadDir: "data/upgrade",
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
	return m
}

// DownloadDir 下载目录
func (m *Manager) DownloadDir() string { return m.downloadDir }

// SetUpgradeHook 注入升级应用钩子（换二进制逻辑，由上层实现）
func (m *Manager) SetUpgradeHook(f func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upgradeHook = f
}

// SetRestartHook 注入重启钩子（watchdog 自动恢复用）
func (m *Manager) SetRestartHook(f func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restartHook = f
}

// MarkUpgraded 记录一次成功升级
func (m *Manager) MarkUpgraded() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastUpgrade = UpgradeRecord{
		Upgraded: true,
		Version:  time.Now().Format("2006.01.02"),
		At:       time.Now().Format("2006-01-02 15:04:05"),
	}
}

// LastUpgrade 最近升级记录
func (m *Manager) LastUpgrade() UpgradeRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastUpgrade
}

// CheckHealth 健康检查（watchdog 核心：最小 HTTP 探活）
func (m *Manager) CheckHealth(ctx context.Context) (bool, error) {
	if m.healthURL == "" {
		return true, nil // 未配置则默认健康
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.healthURL, nil)
	if err != nil {
		return false, fmt.Errorf("构建健康检查请求: %w", err)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("健康检查失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK, nil
}

// CheckOnce 执行一次 watchdog 检查：不健康则触发重启钩子
func (m *Manager) CheckOnce(ctx context.Context) {
	ok, err := m.CheckHealth(ctx)
	if err != nil {
		m.mu.Lock()
		m.lastErr = err.Error()
		m.mu.Unlock()
	}
	if !ok {
		m.mu.Lock()
		restart := m.restartHook
		m.mu.Unlock()
		if restart != nil {
			restart() // 自动恢复
		}
	}
}

// StartWatchdog 后台 watchdog 循环（每间隔检查健康）
func (m *Manager) StartWatchdog(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.CheckOnce(ctx)
			}
		}
	}()
}

// ApplyUpgrade 应用升级（保守两阶段：健康检查通过 → 执行升级钩子）
func (m *Manager) ApplyUpgrade(ctx context.Context) error {
	ok, err := m.CheckHealth(ctx)
	if err != nil {
		return fmt.Errorf("升级前健康检查失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("当前服务不健康，拒绝升级")
	}
	m.mu.Lock()
	hook := m.upgradeHook
	m.mu.Unlock()
	if hook == nil {
		return fmt.Errorf("未配置升级钩子")
	}
	if err := hook(); err != nil {
		return fmt.Errorf("应用升级失败: %w", err)
	}
	m.MarkUpgraded()
	return nil
}

// Status 升级状态快照
func (m *Manager) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"健康检查地址":  m.healthURL,
		"下载目录":    m.downloadDir,
		"最近检查":    formatTime(m.lastCheck),
		"最近错误":    m.lastErr,
		"最近升级":    m.lastUpgrade,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// DownloadRelease 下载发布二进制（Agent 智能化：自主下载）
// url 为发布二进制地址，target 为保存文件名
func (m *Manager) DownloadRelease(ctx context.Context, url, target string) error {
	if err := os.MkdirAll(m.downloadDir, 0o755); err != nil {
		return fmt.Errorf("创建下载目录: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载状态 %d", resp.StatusCode)
	}
	dst := filepath.Join(m.downloadDir, filepath.Base(target))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("写入下载文件: %w", err)
	}
	m.mu.Lock()
	m.lastCheck = time.Now()
	m.mu.Unlock()
	return nil
}
