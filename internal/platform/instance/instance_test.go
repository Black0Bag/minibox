package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	// 第一次获取应成功
	l, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("第一次 Acquire 失败: %v", err)
	}
	defer l.Release()

	// 锁文件存在且含 PID
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("读取锁文件失败: %v", err)
	}
	if string(data) == "" {
		t.Error("锁文件为空")
	}

	// 第二次获取应失败（单实例）
	_, err = Acquire(lockPath)
	if err == nil {
		t.Error("第二次 Acquire 应失败但成功了")
	}
}

func TestRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test2.lock")

	l, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release 失败: %v", err)
	}

	// 释放后应能再次获取
	l2, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("释放后再次 Acquire 失败: %v", err)
	}
	defer l2.Release()
}

func TestReleaseNil(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Errorf("nil Lock Release 应返回 nil，got %v", err)
	}
}

func TestPath(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test3.lock")

	l, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	defer l.Release()

	if l.Path() != lockPath {
		t.Errorf("Path = %q, want %q", l.Path(), lockPath)
	}
}
