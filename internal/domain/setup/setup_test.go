package setup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newFileStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
}

func TestWizard_PendingByDefault(t *testing.T) {
	w := New(newFileStore(t))
	need, err := w.NeedWizard()
	if err != nil {
		t.Fatalf("NeedWizard 失败: %v", err)
	}
	if !need {
		t.Error("全新环境应需要向导")
	}
}

func TestWizard_Complete(t *testing.T) {
	store := newFileStore(t)
	w := New(store)
	done, err := w.Complete()
	if err != nil || !done {
		t.Fatalf("首次完成应返回 done=true: %v %v", done, err)
	}
	need, err := w.NeedWizard()
	if err != nil || need {
		t.Errorf("完成后不应再需要向导: need=%v err=%v", need, err)
	}

	// 幂等：再次 Complete 返回 false
	done2, err := w.Complete()
	if err != nil || done2 {
		t.Errorf("重复完成应幂等返回 false: %v %v", done2, err)
	}
	// 新实例（重启）也应读到 completed
	w2 := New(store)
	if st, _ := w2.Status(); st != StatusCompleted {
		t.Errorf("重启后状态 = %v, 期望 completed", st)
	}
}

func TestWizard_StatusString(t *testing.T) {
	if StatusPending.String() != "pending" || StatusCompleted.String() != "completed" {
		t.Error("状态可读名错误")
	}
}

func TestDeviceCredential_GenerateAndVerify(t *testing.T) {
	cred, err := GenerateDeviceCredential()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(cred.Token()) != 64 {
		t.Errorf("凭据长度 = %d, 期望 64 hex 字符", len(cred.Token()))
	}
	if !cred.Verify(cred.Token()) {
		t.Error("正确令牌应通过校验")
	}
	if cred.Verify("wrong-token") {
		t.Error("错误令牌不应通过")
	}
	if cred.Verify("") {
		t.Error("空令牌不应通过")
	}
}

func TestDeviceCredential_Unique(t *testing.T) {
	a, _ := GenerateDeviceCredential()
	b, _ := GenerateDeviceCredential()
	if a.Token() == b.Token() {
		t.Error("两次生成的凭据不应相同")
	}
}

func TestDeviceCredential_LoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "device.cred")
	cred, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	// 权限 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("凭据文件不存在: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("凭据文件权限 = %o, 期望 600", perm)
	}
	// 再次加载应得到同一令牌
	cred2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("二次加载失败: %v", err)
	}
	if cred.Token() != cred2.Token() {
		t.Error("二次加载令牌应一致")
	}
}

func TestDeviceCredential_LoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.cred")
	// 内容过短 → 拒绝（fail-closed，不静默覆盖）
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Error("损坏凭据文件应报错")
	}
}

func TestPathGuard_Default(t *testing.T) {
	g := DefaultPathGuard()
	cases := []struct {
		path string
		want bool
	}{
		{"/etc/passwd", true},
		{"/etc/ssh/sshd_config", true},
		{"/root/.bashrc", true},
		{"/home/user/.ssh/id_rsa", true},
		{"/home/user/.ssh/", true},
		{"/home/user/project/secret.pem", true},
		{"/home/user/project/id_ed25519", true},
		{"/data/minibox.db", false},
		{"/home/user/project/main.go", false},
		{"/tmp/notes.txt", false},
		{"/etc2/普通目录", false}, // 前缀匹配必须带 /

	}
	for _, c := range cases {
		if got := g.IsSensitive(c.path); got != c.want {
			t.Errorf("IsSensitive(%q) = %v, 期望 %v", c.path, got, c.want)
		}
	}
}

func TestPathGuard_Check(t *testing.T) {
	g := DefaultPathGuard()
	if err := g.Check("/home/user/.ssh/config"); err == nil {
		t.Error("敏感路径 Check 应报错")
	}
	var se *SensitivePathError
	if err := g.Check("/home/user/docs/a.md"); err != nil {
		_ = se
		t.Errorf("普通路径不应报错: %v", err)
	}
	_ = se
}

func TestPathGuard_Custom(t *testing.T) {
	g := NewPathGuard("/opt/secret")
	if !g.IsSensitive("/opt/secret/data.json") {
		t.Error("自定义前缀应命中")
	}
	if g.IsSensitive("/opt/secretary/notes.md") {
		t.Error("前缀相似不应误伤")
	}
	if g.IsSensitive("/opt/elsewhere") {
		t.Error("无关路径不应命中")
	}
}

func TestSetup_RoundTrip(t *testing.T) {
	// 模拟真实生命周期：首次启动 → 向导完成 → 验证秒级内的状态一致性
	dir := t.TempDir()
	path := filepath.Join(dir, "setup.json")
	w := New(NewFileStore(path))
	need, _ := w.NeedWizard()
	if !need {
		t.Fatal("应需要向导")
	}
	done, err := w.Complete()
	if err != nil || !done {
		t.Fatalf("Complete 失败: %v %v", done, err)
	}
	time.Sleep(10 * time.Millisecond) // 模拟时间流逝
	w2 := New(NewFileStore(path))
	need2, err := w2.NeedWizard()
	if err != nil || need2 {
		t.Errorf("重读后应完成: need=%v err=%v", need2, err)
	}
}
