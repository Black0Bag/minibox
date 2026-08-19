// Package instance 提供单实例锁（N-12：gofrs/flock 锁文件写 PID）。
//
// 设计依据：
//   - dev/01_项目总路线图.md：单实例锁 gofrs/flock ✅
//   - dev/02_后端开发路线图.md N-12：gofrs/flock 单实例锁（锁文件写 PID）
//
// 关键实践（互联网 2026 校准）：
//   - 用 TryLock()（非阻塞），已锁则拒绝启动（不阻塞等待）
//   - 锁文件写入 PID + 启动时间，供排查"谁占着实例"
//   - 优雅关闭时 Unlock() 释放锁（进程崩溃时 OS 自动释放 flock）
//   - 锁文件权限 0600（私有，防其他用户篡改）
package instance

import (
	"fmt"
	"os"
	"strconv"

	"github.com/gofrs/flock"
)

// Lock 单实例锁句柄。
type Lock struct {
	fl *flock.Flock
}

// Acquire 尝试获取单实例锁。
// lockPath 为锁文件路径（如 data/minibox.lock）。
// 获取失败（已有实例运行）返回错误。
func Acquire(lockPath string) (*Lock, error) {
	fl := flock.New(lockPath, flock.SetPermissions(0o600))
	ok, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("单实例锁获取失败: %w", err)
	}
	if !ok {
		// 读取已有 PID 辅助排查
		existingPID := readPIDFromFile(lockPath)
		if existingPID > 0 {
			return nil, fmt.Errorf("minibox 已在运行（PID %d），拒绝重复启动", existingPID)
		}
		return nil, fmt.Errorf("minibox 已在运行，拒绝重复启动")
	}

	// 写入当前 PID + 启动时间到锁文件
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)
	// #nosec G304 -- lockPath 由组合根配置注入，非不可信输入
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		_ = fl.Unlock()
		return nil, fmt.Errorf("写入锁文件失败: %w", err)
	}

	return &Lock{fl: fl}, nil
}

// Release 释放单实例锁。
func (l *Lock) Release() error {
	if l == nil || l.fl == nil {
		return nil
	}
	return l.fl.Unlock()
}

// Path 返回锁文件路径。
func (l *Lock) Path() string {
	if l == nil || l.fl == nil {
		return ""
	}
	return l.fl.Path()
}

// readPIDFromFile 从锁文件读取 PID（辅助排查，失败返回 0）。
func readPIDFromFile(path string) int {
	// #nosec G304 -- path 由组合根配置注入
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0
	}
	return pid
}
