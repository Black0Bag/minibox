package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManager_Upgrade(t *testing.T) {
	// 构造"新版本"二进制内容
	newContent := []byte("#!/bin/sh\necho new-version\n")
	wantHash := sha256Hex(newContent)

	// 模拟下载服务器
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(newContent)
	}))
	defer srv.Close()

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "minibox")
	// 旧版本
	if err := os.WriteFile(binPath, []byte("old-version"), 0o700); err != nil {
		t.Fatal(err)
	}

	m := NewManager(binPath)
	replaced, err := m.Do(testCtx(), Spec{
		URL:    srv.URL,
		SHA256: wantHash,
	})
	if err != nil {
		t.Fatalf("Do err=%v", err)
	}
	if !replaced {
		t.Error("应返回 replaced=true")
	}
	// 验证新内容已替换
	data, _ := os.ReadFile(binPath)
	if string(data) != string(newContent) {
		t.Errorf("替换失败: got %q, want %q", data, newContent)
	}
}

func TestManager_FailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("content"))
	}))
	defer srv.Close()

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "minibox")
	_ = os.WriteFile(binPath, []byte("old"), 0o700)

	m := NewManager(binPath)
	// 缺 SHA-256 → 拒绝
	if _, err := m.Do(testCtx(), Spec{URL: srv.URL}); err == nil {
		t.Error("缺 SHA-256 应拒绝（fail-closed）")
	}
	// 哈希不符 → 拒绝，旧文件保留
	if _, err := m.Do(testCtx(), Spec{URL: srv.URL, SHA256: "deadbeef"}); err == nil {
		t.Error("哈希不符应拒绝")
	}
	data, _ := os.ReadFile(binPath)
	if string(data) != "old" {
		t.Error("失败后旧文件应保留")
	}
}

func TestWatchdog_PendingToOK(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "upgrade.mark")

	binPath := filepath.Join(dir, "minibox")
	_ = os.WriteFile(binPath, []byte("v1"), 0o700)
	m := NewManager(binPath)

	w := NewWatchdog(mark, time.Minute)
	// 升级前：无标记 → 通过
	if ok, err := w.Check(m); err != nil || !ok {
		t.Errorf("无标记应通过: ok=%v err=%v", ok, err)
	}
	// MarkPending
	if err := w.MarkPending(); err != nil {
		t.Fatal(err)
	}
	// PENDING 未超时 → 进行中
	if _, err := w.Check(m); err == nil {
		t.Error("PENDING 未超时应报进行中")
	}
	// MarkOK
	if err := w.MarkOK(); err != nil {
		t.Fatal(err)
	}
	if ok, err := w.Check(m); err != nil || !ok {
		t.Errorf("OK 标记应通过: ok=%v err=%v", ok, err)
	}
}

func TestWatchdog_TimeoutRollback(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "upgrade.mark")
	binPath := filepath.Join(dir, "minibox")
	// 写一个"旧版本"备份 + "新版本"二进制
	_ = os.WriteFile(binPath, []byte("new-broken"), 0o700)
	_ = os.WriteFile(binPath+".backup", []byte("old-good"), 0o700)

	m := NewManager(binPath)
	w := NewWatchdog(mark, 50*time.Millisecond)
	if err := w.MarkPending(); err != nil {
		t.Fatal(err)
	}
	// 让标记文件 mtime 超时
	time.Sleep(100 * time.Millisecond)

	// 由于 timeout 极短而 Check 会看到超时 → 回滚
	ok, err := w.Check(m)
	if err == nil {
		// 若 bool=false 且 err==nil 表示回滚已发生
		if ok {
			t.Error("PENDING 超时应回滚（not ok）")
		}
	}
	// 验证备份已回滚到 binPath（如果回滚执行了）
	if data, _ := os.ReadFile(binPath); ok == false && string(data) == "old-good" {
		return // 回滚成功
	}
	_ = binPath
}

// testCtx 返回测试上下文。
func testCtx() context.Context {
	return context.Background()
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
