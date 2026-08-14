package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAcquire_CacheHitAndInstall 安装 + 缓存命中。
func TestAcquire_CacheHitAndInstall(t *testing.T) {
	dir := t.TempDir()
	acq := NewAcquirer(dir, 5*time.Second)

	content := "#!/bin/sh\necho hello-tool\n"
	sum := sha256.Sum256([]byte(content))
	spec := ToolSpec{
		Name:      "hello",
		URL:       "file://unused", // 用下载模拟
		SHA256:    hex.EncodeToString(sum[:]),
		MaxSizeMB: 1,
	}

	// 手动放一个"下载源"文件并读出来（模拟下载）
	src := filepath.Join(t.TempDir(), "hello-src")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// 用本地文件替换下载：直接写临时 + rename 验证原子安装路径
	// 实际验证：Acquire 对不存在的缓存会尝试下载（URL 是 file:// 会失败）。
	// 这里改测：预先放入缓存（哈希匹配）→ 命中；放入错误内容 → 哈希不符
	cached := filepath.Join(dir, "hello")
	if err := os.WriteFile(cached, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}

	// 缓存命中
	res, err := acq.Acquire(context.Background(), spec)
	if err != nil {
		t.Fatalf("Acquire 缓存命中 err=%v", err)
	}
	if res.Installed {
		t.Fatal("缓存命中不应标记 Installed")
	}
	if res.Path != cached {
		t.Fatalf("Path=%q, want %q", res.Path, cached)
	}

	// 篡改缓存 → 哈希不符 → Acquire 走下载路径（会失败），返回错误
	if err := os.WriteFile(cached, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = acq.Acquire(context.Background(), spec)
	if err == nil {
		t.Fatal("篡改缓存 + 下载失败应返回错误（fail-closed）")
	}
}

// TestAcquire_FailClosedNoHash 无哈希拒绝下载（fail-closed）。
func TestAcquire_FailClosedNoHash(t *testing.T) {
	acq := NewAcquirer(t.TempDir(), 5*time.Second)
	_, err := acq.Acquire(context.Background(), ToolSpec{Name: "x", URL: "https://example.com/x"})
	if err == nil {
		t.Fatal("无 SHA-256 应拒绝下载（fail-closed）")
	}
}

// TestVerifySHA256File 校验函数。
func TestVerifySHA256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	content := "data"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])

	ok, got, err := verifySHA256File(path, want)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !ok {
		t.Fatalf("匹配应为 true")
	}
	if got != want {
		t.Fatalf("got=%s want=%s", got, want)
	}

	// 错误哈希
	ok, _, _ = verifySHA256File(path, strings.Repeat("0", 64))
	if ok {
		t.Fatal("错误哈希不应匹配")
	}

	// 不存在文件
	ok, got, err = verifySHA256File(filepath.Join(dir, "missing"), want)
	if err != nil {
		t.Fatalf("不存在文件 err=%v", err)
	}
	if ok || got != "" {
		t.Fatal("不存在文件应返回 (false, \"\")")
	}
}

// TestAtomicInstall 原子安装。
func TestAtomicInstall(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	final := filepath.Join(dir, "final")
	if err := os.WriteFile(tmp, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicInstall(tmp, final); err != nil {
		t.Fatalf("atomicInstall err=%v", err)
	}
	data, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("读 final err=%v", err)
	}
	if string(data) != "content" {
		t.Fatalf("final=%q", string(data))
	}
	// tmp 应已消失
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("tmp 应被 rename 移除")
	}
}

// TestIsolatedRun 隔离 PATH 运行已获取工具。
func TestIsolatedRun(t *testing.T) {
	dir := t.TempDir()
	content := "#!/bin/sh\necho isolated-ok\n"
	if err := os.WriteFile(filepath.Join(dir, "tool"), []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := IsolatedRun(context.Background(), dir, "tool", nil, 5*time.Second)
	if err != nil {
		t.Fatalf("IsolatedRun err=%v", err)
	}
	if out != "isolated-ok\n" {
		t.Fatalf("out=%q", out)
	}
}

// TestNewAcquireTool 注册为工具。
func TestNewAcquireTool(t *testing.T) {
	acq := NewAcquirer(t.TempDir(), 5*time.Second)
	tool := NewAcquireTool(acq)
	if tool.Name() != "acquire_tool" {
		t.Fatalf("Name()=%s", tool.Name())
	}
	if tool.Metadata().RiskTier != "high" {
		t.Fatal("应标记 high 风险")
	}
}