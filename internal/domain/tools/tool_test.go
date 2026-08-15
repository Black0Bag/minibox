package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// mockTool 测试用假工具。
type mockTool struct {
	name    string
	meta    Metadata
	out     string
	err     error
	panicOn bool
	invoked int
	mu      sync.Mutex
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.name + " 描述" }
func (m *mockTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (m *mockTool) Metadata() Metadata { return m.meta }

func (m *mockTool) Invoke(_ context.Context, _ json.RawMessage) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invoked++
	if m.panicOn {
		panic("工具内部崩了")
	}
	if m.err != nil {
		return "", m.err
	}
	return m.out, nil
}

var _ Tool = (*mockTool)(nil)

func newRegistryWith(tools ...*mockTool) *Registry {
	r := NewRegistry()
	for _, t := range tools {
		_ = r.Register(t)
	}
	return r
}

// TestRegister 注册 + 禁止表 + 重名。
func TestRegister(t *testing.T) {
	tests := []struct {
		name  string
		tname string
		want  error
	}{
		{"正常注册", "read_file", nil},
		{"禁止表拒绝", "set_permissions", &ForbiddenError{}},
		{"控制面拒绝", "install_package", &ForbiddenError{}},
		{"重名拒绝", "read_file", &DuplicateError{}},
	}

	r := NewRegistry()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.Register(&mockTool{name: tt.tname})
			switch tt.want.(type) {
			case *ForbiddenError:
				var fe *ForbiddenError
				if !errors.As(err, &fe) {
					t.Fatalf("Register(%q) err=%v, want ForbiddenError", tt.tname, err)
				}
			case *DuplicateError:
				var de *DuplicateError
				if !errors.As(err, &de) {
					t.Fatalf("Register(%q) err=%v, want DuplicateError", tt.tname, err)
				}
			case nil:
				if err != nil {
					t.Fatalf("Register(%q) err=%v, want nil", tt.tname, err)
				}
			}
		})
	}
}

// TestGet_AliasAndCanonical 别名解析。
func TestGet_AliasAndCanonical(t *testing.T) {
	r := newRegistryWith(&mockTool{name: "read_file"})
	r.RegisterAlias("ls", "read_file")

	if _, ok := r.Get("ls"); !ok {
		t.Fatal("别名 ls 应解析到 read_file")
	}
	if _, ok := r.Get("read_file"); !ok {
		t.Fatal("规范名 read_file 应存在")
	}
	if _, ok := r.Get("不存在"); ok {
		t.Fatal("不存在工具不应命中")
	}
}

// TestListAndOrder 注册顺序保持。
func TestListAndOrder(t *testing.T) {
	r := newRegistryWith(
		&mockTool{name: "a"},
		&mockTool{name: "b"},
		&mockTool{name: "c"},
	)
	got := r.Names()
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("Names() len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names()[%d]=%q, want %q", i, got[i], want[i])
		}
	}
	if r.Count() != 3 {
		t.Fatalf("Count()=%d, want 3", r.Count())
	}
}

// TestSubset 白名单子集。
func TestSubset(t *testing.T) {
	r := newRegistryWith(
		&mockTool{name: "a"},
		&mockTool{name: "b"},
		&mockTool{name: "c"},
	)
	sub := r.Subset([]string{"a", "c"})
	if sub.Count() != 2 {
		t.Fatalf("Subset Count()=%d, want 2", sub.Count())
	}
	if _, ok := sub.Get("b"); ok {
		t.Fatal("Subset 不应包含 b")
	}
	if _, ok := sub.Get("a"); !ok {
		t.Fatal("Subset 应包含 a")
	}
}

// TestSafeInvoke_Normal 正常调用。
func TestSafeInvoke_Normal(t *testing.T) {
	tool := &mockTool{name: "echo", out: "hello"}
	out, err := SafeInvoke(context.Background(), tool, nil)
	if err != nil {
		t.Fatalf("SafeInvoke err=%v, want nil", err)
	}
	if out != "hello" {
		t.Fatalf("out=%q, want %q", out, "hello")
	}
	if tool.invoked != 1 {
		t.Fatalf("invoked=%d, want 1", tool.invoked)
	}
}

// TestSafeInvoke_PanicRecover panic 恢复。
func TestSafeInvoke_PanicRecover(t *testing.T) {
	tool := &mockTool{name: "boom", panicOn: true}
	_, err := SafeInvoke(context.Background(), tool, nil)
	var pe *ToolPanicError
	if !errors.As(err, &pe) {
		t.Fatalf("err=%v, want ToolPanicError", err)
	}
}

// TestSafeInvoke_UnderlyingError 透传底层错误。
func TestSafeInvoke_UnderlyingError(t *testing.T) {
	sentinel := errors.New("底层错误")
	tool := &mockTool{name: "f", err: sentinel}
	_, err := SafeInvoke(context.Background(), tool, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want %v", err, sentinel)
	}
}

// TestSafeInvoke_OutputCap 输出上限。
func TestSafeInvoke_OutputCap(t *testing.T) {
	tool := &mockTool{name: "big", out: "1234567890", meta: Metadata{MaxResultSize: 5}}
	_, err := SafeInvoke(context.Background(), tool, nil)
	var oe *ToolOutputTooLargeError
	if !errors.As(err, &oe) {
		t.Fatalf("err=%v, want ToolOutputTooLargeError", err)
	}
	if oe.Max != 5 || oe.Got != 10 {
		t.Fatalf("oe.Got=%d Max=%d, want 10/5", oe.Got, oe.Max)
	}
}

// TestSafeInvoke_NoCap 未配置上限不限制。
func TestSafeInvoke_NoCap(t *testing.T) {
	tool := &mockTool{name: "noCap", out: "任意长度输出"}
	out, err := SafeInvoke(context.Background(), tool, nil)
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if out != "任意长度输出" {
		t.Fatalf("out=%q", out)
	}
}

// TestRegistry_ConcurrentGet 并发读安全（golang-safety：map 并发读写 panic）。
func TestRegistry_ConcurrentGet(t *testing.T) {
	r := newRegistryWith(&mockTool{name: "read_file"})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := r.Get("read_file"); !ok {
				t.Error("并发 Get 失败")
			}
		}()
	}
	wg.Wait()
}

// TestIsForbidden 禁止表查询。
func TestIsForbidden(t *testing.T) {
	if !IsForbidden("set_permissions") {
		t.Fatal("set_permissions 应在禁止表")
	}
	if IsForbidden("read_file") {
		t.Fatal("read_file 不应在禁止表")
	}
}
