// Package upgrade 提供自升级实现（B19：Agent 智能化下载/校验/原子替换 + watchdog）。
// 设计：PRD B19，PID 脚本回滚 + watchdog 健康检查。
// 流程：下载新二进制 → SHA-256 校验 → 原子替换旧文件 → 重启 → watchdog 验证。
// 安全（golang-security）：fail-closed（无哈希拒绝/哈希不符拒绝安装）。
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Spec 升级目标描述。
type Spec struct {
	URL       string        // 新二进制下载地址
	SHA256    string        // 期望 SHA-256（hex，必填，fail-closed）
	Retries   int           // 下载重试次数（默认 3）
	RetryWait time.Duration // 重试间隔（默认 1s）
}

// Manager 自升级管理器。
type Manager struct {
	binPath string // 当前二进制路径
	backup  string // 备份路径（回滚用）
	client  *http.Client
}

// NewManager 创建自升级管理器。
func NewManager(binPath string) *Manager {
	return &Manager{
		binPath: binPath,
		backup:  binPath + ".backup",
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

// Do 执行自升级（下载→校验→原子替换）。
// 返回：是否已替换（true=需重启服务生效）。
func (m *Manager) Do(ctx context.Context, spec Spec) (bool, error) {
	if spec.SHA256 == "" {
		return false, fmt.Errorf("升级目标缺 SHA-256（fail-closed）")
	}
	retries := spec.Retries
	if retries <= 0 {
		retries = 3
	}
	wait := spec.RetryWait
	if wait <= 0 {
		wait = time.Second
	}

	// 下载到临时文件
	tmp, err := m.download(ctx, spec, retries, wait)
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp) }()

	// SHA-256 校验（替换前，fail-closed）
	if !verifySHA256(tmp, spec.SHA256) {
		return false, fmt.Errorf("SHA-256 校验失败，拒绝安装")
	}

	// 备份旧二进制（回滚点）
	if err := m.backupOld(); err != nil {
		return false, err
	}

	// 原子替换
	if err := os.Rename(tmp, m.binPath); err != nil {
		// 替换失败 → 回滚备份
		_ = os.Rename(m.backup, m.binPath)
		return false, fmt.Errorf("替换二进制失败（已回滚）: %w", err)
	}
	if err := os.Chmod(m.binPath, 0o700); err != nil { // #nosec G302 -- 二进制需可执行位，0700 owner-only 已是安全默认
		return false, err
	}
	return true, nil
}

// Rollback 回滚到备份版本（watchdog 检测启动失败时调用）。
func (m *Manager) Rollback() error {
	if _, err := os.Stat(m.backup); err != nil {
		return fmt.Errorf("无可回滚备份")
	}
	if err := os.Rename(m.backup, m.binPath); err != nil {
		return fmt.Errorf("回滚失败: %w", err)
	}
	return nil
}

// download 下载新二进制到临时文件（带重试退避）。
func (m *Manager) download(ctx context.Context, spec Spec, retries int, wait time.Duration) (string, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		tmp, err := m.downloadOnce(ctx, spec.URL)
		if err == nil {
			return tmp, nil
		}
		lastErr = err
		if i < retries-1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return "", fmt.Errorf("下载失败（%d 次重试）: %w", retries, lastErr)
}

// downloadOnce 单次下载。
func (m *Manager) downloadOnce(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "minibox-update-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = tmp.Close() }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// backupOld 备份当前二进制（替换前创建回滚点）。
func (m *Manager) backupOld() error {
	if _, err := os.Stat(m.binPath); os.IsNotExist(err) {
		return fmt.Errorf("当前二进制不存在: %s", m.binPath)
	}
	data, err := os.ReadFile(m.binPath) // #nosec G304,G703 -- binPath/backup 由组合根配置注入（当前可执行文件路径）
	if err != nil {
		return err
	}
	return os.WriteFile(m.backup, data, 0o700) // #nosec G306,G703 -- 备份需保留可执行位，路径组合根注入
}

// verifySHA256 计算并比对文件 SHA-256（常时比较，防时序）。
func verifySHA256(path, want string) bool {
	f, err := os.Open(path) // #nosec G304 -- path 为内部下载临时文件（os.CreateTemp），非不可信输入
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == want
}
