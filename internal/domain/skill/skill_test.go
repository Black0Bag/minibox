package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestSkill 写一个测试 skill 目录（含 SKILL.md + 资源文件）。
func writeTestSkill(t *testing.T, body, resource string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if resource != "" {
		if err := os.WriteFile(filepath.Join(dir, "ref.md"), []byte(resource), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLevel1 元数据精简（~100 token）。
func TestLevel1(t *testing.T) {
	dir := writeTestSkill(t, "# 做代码评审\nUse when: 需要评审代码", "")
	s := &Skill{Meta: Metadata{Name: "code-review", Description: "做代码评审 Use when 需要评审代码"}, Root: dir}
	level1 := s.Level1()
	if !strings.Contains(level1, "code-review") || !strings.Contains(level1, "做代码评审") {
		t.Errorf("Level1 元数据错误: %q", level1)
	}
}

// TestLoadBody_AppendOnly 加载 body（缓存纯追加，不覆盖）。
func TestLoadBody_AppendOnly(t *testing.T) {
	dir := writeTestSkill(t, "# Skill body\ncontent", "")
	s := &Skill{Meta: Metadata{Name: "s"}, Root: dir}

	body, err := s.LoadBody()
	if err != nil {
		t.Fatalf("LoadBody err=%v", err)
	}
	if !strings.Contains(body, "Skill body") {
		t.Errorf("body 错误: %q", body)
	}
	if !s.bodyLoaded {
		t.Error("加载后应标记 bodyLoaded")
	}

	// 再次加载返回缓存（不重复读盘）
	body2, _ := s.LoadBody()
	if body2 != body {
		t.Error("第二次应返回缓存")
	}
}

// TestReadFile 读 skill 资源（Level3）。
func TestReadFile(t *testing.T) {
	dir := writeTestSkill(t, "body", "# 参考资料")
	s := &Skill{Root: dir}
	data, err := s.ReadFile("ref.md")
	if err != nil {
		t.Fatalf("ReadFile err=%v", err)
	}
	if !strings.Contains(data, "参考资料") {
		t.Errorf("资源内容错误: %q", data)
	}
}

// TestReadFile_PathTraversal 路径穿越防护。
func TestReadFile_PathTraversal(t *testing.T) {
	dir := writeTestSkill(t, "body", "")
	s := &Skill{Root: dir}
	// 越界路径
	if _, err := s.ReadFile("../secret.txt"); err == nil {
		t.Fatal("路径穿越应被拒绝")
	}
	if _, err := s.ReadFile("/etc/passwd"); err == nil {
		t.Fatal("绝对路径越界应被拒绝")
	}
}

// TestRegistry 注册/获取/列出。
func TestRegistry(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&Skill{Meta: Metadata{Name: "a"}})
	_ = r.Register(&Skill{Meta: Metadata{Name: "b"}})

	// 重名拒绝
	if err := r.Register(&Skill{Meta: Metadata{Name: "a"}}); err == nil {
		t.Fatal("重名应报错")
	}

	if r.Count() != 2 {
		t.Errorf("Count=%d, 期望 2", r.Count())
	}
	if _, ok := r.Get("a"); !ok {
		t.Error("Get a 应成功")
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("Get missing 应失败")
	}
	if len(r.List()) != 2 {
		t.Errorf("List 长度=%d, 期望 2", len(r.List()))
	}
}
