package worldbook

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestWorldbook 写一个测试世界书目录。
func writeTestWorldbook(t *testing.T, pluginJSON string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, []byte(pluginJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLoad_Basic 加载基本世界书。
func TestLoad_Basic(t *testing.T) {
	dir := writeTestWorldbook(t, `{"id":"dev","name":"开发模式","version":"1.0","skills":["go-skill"]}`)
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if p.Meta.ID != "dev" || p.Meta.Name != "开发模式" {
		t.Errorf("元数据错误: %+v", p.Meta)
	}
	if !p.Enabled {
		t.Error("加载后应启用")
	}
}

// TestLoad_Extension 扩展命名空间。
func TestLoad_Extension(t *testing.T) {
	json := `{"id":"x","name":"X","version":"1.0",
		"io.github.zsm.minibox":{"ui":["theme-dark"],"tools":["read_file"],"system_prompt":["work-prompt"]}}`
	dir := writeTestWorldbook(t, json)
	loader := New(Hooks{})
	p, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if p.Meta.Extension == nil {
		t.Fatal("扩展命名空间应为 nil")
	}
	if len(p.Meta.Extension.Tools) != 1 || p.Meta.Extension.Tools[0] != "read_file" {
		t.Errorf("扩展 tools 错误: %v", p.Meta.Extension.Tools)
	}
}

// TestLoad_Hooks 触发 P1-P8 接口。
func TestLoad_Hooks(t *testing.T) {
	json := `{"id":"h","name":"H","version":"1.0","skills":["s1"],"mcp":["m1"],
		"io.github.zsm.minibox":{"tools":["t1"],"system_prompt":["sp1"]}}`
	dir := writeTestWorldbook(t, json)

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
	dir := writeTestWorldbook(t, `{not-json`)
	loader := New(Hooks{})
	if _, err := loader.Load(dir); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

// TestDisable 关闭世界书。
func TestDisable(t *testing.T) {
	dir := writeTestWorldbook(t, `{"id":"x","name":"X","version":"1.0"}`)
	loader := New(Hooks{})
	p, _ := loader.Load(dir)
	p.Disable()
	if p.Enabled {
		t.Error("Disable 后应关闭")
	}
}
