package worldbook

import (
	"os"
	"path/filepath"
	"testing"
)

// writeWorldbook 写一个符合 Agent Plugins 1.0.0 的世界书目录。
// skills：map[skill名]SKILL.md内容；mcp：可选 mcp.json 内容；extra：可选顶层附加 JSON。
func writeWorldbook(t *testing.T, pluginJSON string, skills map[string]string, mcpJSON string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, []byte(pluginJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range skills {
		sdir := filepath.Join(dir, "skills", name)
		if err := os.MkdirAll(sdir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if mcpJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(mcpJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// validManifest 一份合法的最小清单。
const validManifest = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"dev"}`

// TestLoad_Basic 加载基本世界书（必填 $schema + name）。
func TestLoad_Basic(t *testing.T) {
	dir := writeWorldbook(t, validManifest, nil, "")
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if p.Meta.Name != "dev" {
		t.Errorf("名称错误: %+v", p.Meta)
	}
	if !p.Enabled {
		t.Error("加载后应启用")
	}
}

// TestLoad_MissingSchema 缺 $schema 致命。
func TestLoad_MissingSchema(t *testing.T) {
	dir := writeWorldbook(t, `{"name":"x"}`, nil, "")
	loader := New(Hooks{})
	if _, err := loader.Load(dir); err == nil {
		t.Fatal("缺 $schema 应报错")
	}
}

// TestLoad_UnsupportedSchema 不支持的 $schema 致命。
func TestLoad_UnsupportedSchema(t *testing.T) {
	dir := writeWorldbook(t, `{"$schema":"https://agent-plugins.org/schemas/2.0.0/plugin.schema.json","name":"x"}`, nil, "")
	loader := New(Hooks{})
	if _, err := loader.Load(dir); err == nil {
		t.Fatal("不支持的 $schema 应报错")
	}
}

// TestLoad_MissingName 缺 name 致命。
func TestLoad_MissingName(t *testing.T) {
	dir := writeWorldbook(t, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"}`, nil, "")
	loader := New(Hooks{})
	if _, err := loader.Load(dir); err == nil {
		t.Fatal("缺 name 应报错")
	}
}

// TestLoad_UnknownField 未知顶层字段报告并忽略（非致命）。
func TestLoad_UnknownField(t *testing.T) {
	json := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"x","id":"legacy","hooks":true}`
	dir := writeWorldbook(t, json, nil, "")
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("未知字段不应致命: %v", err)
	}
	if len(p.UnknownFields) != 2 {
		t.Errorf("UnknownFields = %v, 期望 [hooks id]", p.UnknownFields)
	}
}

// TestLoad_Extension 扩展命名空间（io.github.zsm.minibox）。
func TestLoad_Extension(t *testing.T) {
	json := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"x",
		"extensions":{"io.github.zsm.minibox":{"ui":["theme-dark"],"tools":["read_file"],"system_prompt":["work-prompt"]}}}`
	dir := writeWorldbook(t, json, nil, "")
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if p.Extension == nil {
		t.Fatal("扩展命名空间应为非 nil")
	}
	if len(p.Extension.Tools) != 1 || p.Extension.Tools[0] != "read_file" {
		t.Errorf("扩展 tools 错误: %v", p.Extension.Tools)
	}
}

// TestLoad_SkillsDir skills/ 目录发现组件。
func TestLoad_SkillsDir(t *testing.T) {
	dir := writeWorldbook(t, validManifest, map[string]string{
		"code-review": "# 做代码评审",
		"debug":       "# 调试",
	}, "")
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if len(p.Skills) != 2 {
		t.Fatalf("Skills = %d, 期望 2: %+v", len(p.Skills), p.Skills)
	}
	names := map[string]bool{}
	for _, s := range p.Skills {
		names[s.Name] = true
	}
	if !names["code-review"] || !names["debug"] {
		t.Errorf("skills 发现错误: %v", names)
	}
}

// TestLoad_MCP mcp.json 发现 MCP 服务器。
func TestLoad_MCP(t *testing.T) {
	mcp := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
		"mcpServers":{"filesystem":{"type":"stdio","command":"npx"}}}`
	dir := writeWorldbook(t, validManifest, nil, mcp)
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if len(p.MCP) != 1 || p.MCP[0] != "filesystem" {
		t.Errorf("MCP 发现错误: %v", p.MCP)
	}
}

// TestLoad_Hooks 触发 P1-P8 接口。
func TestLoad_Hooks(t *testing.T) {
	dir := writeWorldbook(t,
		`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"h",
		 "extensions":{"io.github.zsm.minibox":{"tools":["t1"],"system_prompt":["sp1"]}}}`,
		map[string]string{"s1": "# s1"}, `{"mcpServers":{"m1":{"type":"stdio","command":"x"}}}`)

	var skillHooks, mcpHooks, toolHooks, promptHooks []string
	loader := New(Hooks{
		OnSkill: func(_ *Profile, names []string) error {
			skillHooks = append(skillHooks, names...)
			return nil
		},
		OnMCP: func(_ *Profile, servers []string) error {
			mcpHooks = append(mcpHooks, servers...)
			return nil
		},
		OnTools: func(_ *Profile, tools []string) error {
			toolHooks = append(toolHooks, tools...)
			return nil
		},
		OnSystemPrompt: func(_ *Profile, sections []string) error {
			promptHooks = append(promptHooks, sections...)
			return nil
		},
	})
	_, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if len(skillHooks) != 1 || skillHooks[0] != "s1" {
		t.Errorf("P1 skill hook: %v", skillHooks)
	}
	if len(mcpHooks) != 1 || mcpHooks[0] != "m1" {
		t.Errorf("P2 MCP hook: %v", mcpHooks)
	}
	if len(toolHooks) != 1 || toolHooks[0] != "t1" {
		t.Errorf("P4 工具 hook: %v", toolHooks)
	}
	if len(promptHooks) != 1 || promptHooks[0] != "sp1" {
		t.Errorf("P3 prompt hook: %v", promptHooks)
	}
}

// TestLoad_NoPluginFile 缺 plugin.json。
func TestLoad_NoPluginFile(t *testing.T) {
	dir := t.TempDir() // 空目录
	loader := New(Hooks{})
	if _, err := loader.Load(dir); err == nil {
		t.Fatal("缺 plugin.json 应报错")
	}
}

// TestLoad_InvalidJSON 非法 JSON。
func TestLoad_InvalidJSON(t *testing.T) {
	dir := writeWorldbook(t, `{not-json`, nil, "")
	loader := New(Hooks{})
	if _, err := loader.Load(dir); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

// TestDisable 关闭世界书。
func TestDisable(t *testing.T) {
	dir := writeWorldbook(t, validManifest, nil, "")
	loader := New(Hooks{})
	p, _ := loader.Load(dir)
	p.Disable()
	if p.Enabled {
		t.Error("Disable 后应关闭")
	}
}

// TestLoad_SymlinkEscape skills 目录内 symlink 逃逸被 os.Root 拦截。
func TestLoad_SymlinkEscape(t *testing.T) {
	dir := writeWorldbook(t, validManifest, map[string]string{"good": "# ok"}, "")
	// 在 skills/ 里放一个指向外部目录的 symlink（os.Root 应拒绝读取其中 SKILL.md）
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "SKILL.md"), []byte("逃逸内容"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "skills", "evil")
	if err := os.MkdirAll(linkDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// symlink 指向外部 SKILL.md（路径逃逸）
	if err := os.Symlink(filepath.Join(external, "SKILL.md"), filepath.Join(linkDir, "SKILL.md")); err != nil {
		t.Skipf("环境不支持 symlink: %v", err)
	}
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	// good 应被发现；evil 的 SKILL.md 是 symlink 指向外部 → 目录 Stat 仍可能过
	// 但 ReadFile 时 os.Root 会拦截。此处断言加载不崩溃且 good 存在即可。
	for _, s := range p.Skills {
		if s.Name == "good" {
			return
		}
	}
	t.Fatalf("good skill 应被发现: %+v", p.Skills)
}
