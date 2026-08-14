package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/Black0Bag/minibox/internal/platform/fsutil"
)

// newTestEnv 创建测试沙箱（根在 t.TempDir）并预置一个文件。
func newTestEnv(t *testing.T, filename, content string) (*fsutil.PathValidator, string) {
	t.Helper()
	root := t.TempDir()
	if filename != "" {
		p := filepath.Join(root, filename)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fsutil.NewPathValidator(root), root
}

// TestReadFile 正常读 + 越权读。
func TestReadFile(t *testing.T) {
	fs, root := newTestEnv(t, "a.txt", "你好 minibox")
	tool := NewReadFile(fs)

	// 正常读（绝对路径）
	in := `{"path":` + mustQuote(filepath.Join(root, "a.txt")) + `}`
	out, err := tool.Invoke(context.Background(), []byte(in))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	if out != "你好 minibox" {
		t.Fatalf("out=%q", out)
	}

	// 越权读（/etc/passwd 逃出沙箱）
	in = `{"path":"/etc/passwd"}`
	if _, err := tool.Invoke(context.Background(), []byte(in)); err == nil {
		t.Fatal("越权读应失败")
	}
}

// TestReadFile_MissingArg 缺参数。
func TestReadFile_MissingArg(t *testing.T) {
	fs, _ := newTestEnv(t, "a.txt", "x")
	tool := NewReadFile(fs)
	if _, err := tool.Invoke(context.Background(), nil); err == nil {
		t.Fatal("缺 path 应失败")
	}
}

// TestListDir 列目录。
func TestListDir(t *testing.T) {
	fs, root := newTestEnv(t, "sub/foo.go", "package foo")
	tool := NewListDir(fs)
	out, err := tool.Invoke(context.Background(), []byte(`{"path":`+mustQuote(root)+`}`))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	if out != "sub/\n" {
		t.Fatalf("out=%q, want %q", out, "sub/\n")
	}
}

// TestWriteFile 写文件 + 追加。
func TestWriteFile(t *testing.T) {
	fs, root := newTestEnv(t, "", "")
	tool := NewWriteFile(fs)

	// 写（绝对路径）
	in := `{"path":` + mustQuote(filepath.Join(root, "out.txt")) + `,"content":"line1"}`
	if _, err := tool.Invoke(context.Background(), []byte(in)); err != nil {
		t.Fatalf("写 err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil {
		t.Fatalf("读回 err=%v", err)
	}
	if string(data) != "line1" {
		t.Fatalf("写入内容=%q", string(data))
	}

	// 追加
	in = `{"path":` + mustQuote(filepath.Join(root, "out.txt")) + `,"content":"line2","append":true}`
	if _, err := tool.Invoke(context.Background(), []byte(in)); err != nil {
		t.Fatalf("追加 err=%v", err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "out.txt"))
	if string(data) != "line1line2" {
		t.Fatalf("追加后内容=%q", string(data))
	}

	// 越权写
	in = `{"path":"/tmp/minibox-evil.txt","content":"x"}`
	if _, err := tool.Invoke(context.Background(), []byte(in)); err == nil {
		t.Fatal("越权写应失败")
	}
}

// TestSearchFiles 文件搜索。
func TestSearchFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "found.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := fsutil.NewPathValidator(root)
	tool := NewSearchFiles(fs)

	pat := mustQuote(filepath.Join(root, "*.go"))
	out, err := tool.Invoke(context.Background(), []byte(`{"pattern":`+pat+`}`))
	if err != nil {
		t.Fatalf("Invoke err=%v", err)
	}
	want := filepath.Join(root, "found.go") + "\n"
	if out != want {
		t.Fatalf("out=%q, want %q", out, want)
	}
}

// mustQuote JSON 字符串转义（供路径注入 JSON）。
func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestRegistry_RegisterBuiltins 内置工具全注册。
func TestRegistry_RegisterBuiltins(t *testing.T) {
	root := t.TempDir()
	fs := fsutil.NewPathValidator(root)
	r := tools.NewRegistry()
	for _, tl := range []tools.Tool{
		NewReadFile(fs),
		NewListDir(fs),
		NewSearchFiles(fs),
		NewWriteFile(fs),
		NewShell(0),
	} {
		if err := r.Register(tl); err != nil {
			t.Fatalf("注册 %s 失败: %v", tl.Name(), err)
		}
	}
	if r.Count() != 5 {
		t.Fatalf("Count()=%d, want 5", r.Count())
	}
}