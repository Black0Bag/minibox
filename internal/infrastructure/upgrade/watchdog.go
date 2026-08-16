package upgrade

// watchdog：升级后健康检查，启动失败自动回滚（B19）。
// 流程：升级前写 PENDING 标记 → 新二进制启动后写 OK 标记 → watchdog 若
// 超时未 OK 则回滚备份并重启。PID 脚本回滚（写入 PID + 标记文件）。

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Watchdog 自升级 watchdog。
type Watchdog struct {
	markFile string // 标记文件路径（如 data/upgrade.mark）
	timeout  time.Duration
}

// NewWatchdog 创建 watchdog。
func NewWatchdog(markFile string, timeout time.Duration) *Watchdog {
	return &Watchdog{markFile: markFile, timeout: timeout}
}

// MarkPending 升级前写 PENDING 标记（+ PID）。
func (w *Watchdog) MarkPending() error {
	dir := filepath.Dir(w.markFile)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("创建标记目录失败: %w", err)
	}
	content := fmt.Sprintf("pending pid=%d\n", os.Getpid())
	return os.WriteFile(w.markFile, []byte(content), 0o600)
}

// MarkOK 新进程启动成功后写 OK（watchdog 见到 OK 即放行）。
func (w *Watchdog) MarkOK() error {
	content := fmt.Sprintf("ok pid=%d ts=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(w.markFile, []byte(content), 0o600)
}

// Check 检查升级是否成功：PENDING 超过超时未转 OK → 需要回滚。
// 返回 true 表示升级成功（OK 标记）；false 表示超时待回滚。
func (w *Watchdog) Check(mgr *Manager) (bool, error) {
	data, err := os.ReadFile(w.markFile) // #nosec G304 -- markFile 由组合根配置注入
	if os.IsNotExist(err) {
		return true, nil // 无标记 = 非升级流程，直接通过
	}
	if err != nil {
		return false, err
	}
	ok := len(data) > 0 && data[0] == 'o' // "ok" 开头
	if ok {
		return true, nil
	}
	// pending：检查超时
	info, err := os.Stat(w.markFile)
	if err != nil {
		return false, err
	}
	if time.Since(info.ModTime()) > w.timeout {
		// 超时未 OK → 回滚备份
		if mgr != nil {
			if rbErr := mgr.Rollback(); rbErr != nil {
				return false, fmt.Errorf("回滚失败: %w; 原始: %s", rbErr, data)
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("升级进行中（PENDING 未超时）")
}
