package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogme_Append(t *testing.T) {
	dir := t.TempDir()
	l := NewLogme(dir, 0)
	if err := l.Log("agent.step", "trace-001", "planning"); err != nil {
		t.Fatalf("Log err=%v", err)
	}
	if err := l.Log("tool.invoke", "trace-001", "read_file"); err != nil {
		t.Fatalf("Log err=%v", err)
	}

	// 读取文件验证
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, today+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 logme 文件失败: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有 2 行，实际 %d: %s", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "agent.step") || !strings.Contains(lines[0], "trace-001") {
		t.Errorf("第一条记录错误: %s", lines[0])
	}
	if !strings.Contains(lines[1], "tool.invoke") {
		t.Errorf("第二条记录错误: %s", lines[1])
	}
}

func TestLogme_CleanExpired(t *testing.T) {
	dir := t.TempDir()
	// 创建旧日志文件
	oldPath := filepath.Join(dir, "2026-01-01.log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 把 mtime 设到很久以前
	if err := os.Chtimes(oldPath, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour)); err != nil {
		t.Skipf("Chtimes 不支持: %v", err)
	}

	l := NewLogme(dir, 24*time.Hour)
	removed, err := l.CleanExpired()
	if err != nil {
		t.Fatalf("CleanExpired err=%v", err)
	}
	if removed != 1 {
		t.Errorf("应清理 1 个过期文件，实际 %d", removed)
	}
}

func TestLogme_Permission(t *testing.T) {
	dir := t.TempDir()
	l := NewLogme(dir, 0)
	if err := l.Log("test", "t", "x"); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	info, err := os.Stat(filepath.Join(dir, today+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("logme 权限应为 600，实际 %o", perm)
	}
}
