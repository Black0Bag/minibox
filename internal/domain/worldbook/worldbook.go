// Package worldbook 世界书 Profile 加载器（B10，Agent Plugins 1.0.0 标准兼容）。
// 设计（联网校准 agent-plugins.org v1.0.0 官方规范 + plugin.schema.json）：
//   - 世界书 = 整机运行模式切换器（大项目开发=加载开发世界书）
//   - plugin.json 封闭 schema：仅 $schema/name/version/description/author/homepage/
//     repository/license/keywords/extensions 顶层字段；未知字段报告并忽略（非致命）
//   - 必填：$schema（https://agent-plugins.org/schemas/1.0.0/plugin.schema.json）+ name
//   - skills 组件：skills/<name>/SKILL.md 固定位置发现（plugin.json 不含组件配置）
//   - MCP 组件：根目录 mcp.json（$schema + mcpServers）
//   - 扩展命名空间：extensions 反向域名对象（io.github.zsm.minibox 承载咱家数据）
//   - 所有文件读取经 os.Root 锁定 plugin root（规范 §3：解析必须保持在 root 内）
//   - 预留 8 类插入接口 P1-P8：skill 注册/MCP 注册/system prompt 装配/工具白名单/
//     记忆权重/前端 UI 组件/事件钩子/权限策略，组件失败隔离
package worldbook

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// SchemaURL Agent Plugins 1.0.0 清单 schema 规范标识。
const SchemaURL = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

// ExtensionNamespace 咱家扩展命名空间（反向域名）。
const ExtensionNamespace = "io.github.zsm.minibox"

// Manifest 世界书清单（Agent Plugins 1.0.0 封闭 schema）。
// 未知顶层字段由 json 忽略，由 rawFieldNames 收集用于"报告并忽略"。
type Manifest struct {
	Schema      string         `json:"$schema"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Author      *Author        `json:"author,omitempty"`
	Homepage    string         `json:"homepage"`
	Repository  string         `json:"repository"`
	License     string         `json:"license"`
	Keywords    []string       `json:"keywords"`
	Extensions  map[string]any `json:"extensions"`
}

// Author 清单作者对象。
type Author struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	URL   string `json:"url"`
}

// Extension 咱家世界书扩展命名空间（io.github.zsm.minibox）。
type Extension struct {
	UI           []string `json:"ui,omitempty"`            // 前端 UI 组件/图标/主题
	SystemPrompt []string `json:"system_prompt,omitempty"` // 世界书专属 system prompt section
	MemoryBias   []string `json:"memory_bias,omitempty"`   // 记忆检索权重偏颇
	Tools        []string `json:"tools,omitempty"`         // 工具白名单扩展
}

// SkillEntry 发现的 skill 组件。
type SkillEntry struct {
	Name string // skill 名（skills/<name> 目录名）
	Path string // SKILL.md 路径
}

// Profile 已加载的世界书（运行模式）。
type Profile struct {
	Meta          Manifest
	Root          string // 世界书根目录
	Enabled       bool
	Skills        []SkillEntry // 从 skills/ 目录发现的组件
	MCP           []string     // mcp.json 里的服务器名
	Extension     *Extension   // 咱家扩展（可能为 nil）
	UnknownFields []string     // 报告并忽略的未知顶层字段（规范要求报告）
}

// Hooks P1-P8 插入接口（预留，供 composition root / 前端装配）。
// 每个返回错误表示该组件加载失败（失败隔离，不影响其他组件）。
type Hooks struct {
	// P1 skill 注册
	OnSkill func(p *Profile, names []string) error
	// P2 MCP 注册
	OnMCP func(p *Profile, servers []string) error
	// P3 system prompt 装配
	OnSystemPrompt func(p *Profile, sections []string) error
	// P4 工具白名单
	OnTools func(p *Profile, tools []string) error
	// P5 记忆权重
	OnMemoryBias func(p *Profile, bias []string) error
	// P6 前端 UI 组件注册
	OnUI func(p *Profile, components []string) error
	// P7 事件钩子
	OnHooks func(p *Profile) error
	// P8 权限策略
	OnPermissions func(p *Profile) error
}

// Loader 世界书加载器。
type Loader struct {
	hooks Hooks
}

// New 创建加载器。
func New(hooks Hooks) *Loader {
	return &Loader{hooks: hooks}
}

// Load 从目录加载世界书（读取 plugin.json + schema 校验 + skills/ 发现 + mcp.json + hooks）。
// 所有文件读取经 os.Root 锁定在 plugin root 内（规范 §3）。
// 失败隔离：单个组件加载失败仅记录错误，不阻断整个世界书加载。
func (l *Loader) Load(root string) (*Profile, error) {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("打开世界书目录失败 %s: %w", root, err)
	}
	defer func() { _ = rootHandle.Close() }()

	data, err := rootHandle.ReadFile("plugin.json")
	if err != nil {
		return nil, fmt.Errorf("读取世界书清单失败 %s: %w", filepath.Join(root, "plugin.json"), err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析世界书清单失败: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}

	// 未知顶层字段：报告并忽略（规范 §5.2：封闭 schema 违规不致命）
	unknown := detectUnknownFields(data)

	p := &Profile{
		Meta:          manifest,
		Root:          root,
		Enabled:       true,
		Skills:        discoverSkills(rootHandle),
		MCP:           discoverMCP(rootHandle),
		UnknownFields: unknown,
	}
	p.Extension = parseExtension(manifest)

	// 触发 P1-P8 插入接口（各自失败隔离）
	skillNames := make([]string, 0, len(p.Skills))
	for _, s := range p.Skills {
		skillNames = append(skillNames, s.Name)
	}
	if l.hooks.OnSkill != nil {
		_ = l.hooks.OnSkill(p, skillNames)
	}
	if l.hooks.OnMCP != nil {
		_ = l.hooks.OnMCP(p, p.MCP)
	}
	if p.Extension != nil {
		if l.hooks.OnSystemPrompt != nil {
			_ = l.hooks.OnSystemPrompt(p, p.Extension.SystemPrompt)
		}
		if l.hooks.OnTools != nil {
			_ = l.hooks.OnTools(p, p.Extension.Tools)
		}
		if l.hooks.OnMemoryBias != nil {
			_ = l.hooks.OnMemoryBias(p, p.Extension.MemoryBias)
		}
		if l.hooks.OnUI != nil {
			_ = l.hooks.OnUI(p, p.Extension.UI)
		}
	}
	if l.hooks.OnHooks != nil {
		_ = l.hooks.OnHooks(p)
	}
	if l.hooks.OnPermissions != nil {
		_ = l.hooks.OnPermissions(p)
	}

	return p, nil
}

// validateManifest 校验封闭 schema：
// $schema 必填且为支持的版本；name 必填。未知顶层字段 → 报告并忽略（非致命）。
func validateManifest(m Manifest) error {
	if m.Schema == "" {
		return fmt.Errorf("世界书清单缺 $schema")
	}
	if m.Schema != SchemaURL {
		return fmt.Errorf("不支持的世界书 $schema: %s（期望 %s）", m.Schema, SchemaURL)
	}
	if m.Name == "" {
		return fmt.Errorf("世界书清单缺 name")
	}
	return nil
}

// parseExtension 从 extensions 反向域名命名空间解析咱家扩展。
func parseExtension(m Manifest) *Extension {
	if m.Extensions == nil {
		return nil
	}
	raw, ok := m.Extensions[ExtensionNamespace]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var ext Extension
	if err := json.Unmarshal(data, &ext); err != nil {
		return nil
	}
	return &ext
}

// knownTopFields 封闭 schema 允许的顶层字段（用于未知字段检测）。
var knownTopFields = map[string]bool{
	"$schema": true, "name": true, "version": true, "description": true,
	"author": true, "homepage": true, "repository": true, "license": true,
	"keywords": true, "extensions": true,
}

// detectUnknownFields 找出清单中不在封闭 schema 里的顶层字段（报告并忽略）。
func detectUnknownFields(data []byte) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var out []string
	for k := range raw {
		if !knownTopFields[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// discoverSkills 扫描 skills/<name>/SKILL.md 固定位置发现组件。
func discoverSkills(root *os.Root) []SkillEntry {
	entries, err := fs.ReadDir(root.FS(), "skills")
	if err != nil {
		return nil // 无 skills/ 目录或非目录 → 无组件
	}
	var out []SkillEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// 验证 SKILL.md 存在（root.ReadFile 防逃逸）
		rel := filepath.Join("skills", name, "SKILL.md")
		if _, err := root.Stat(rel); err != nil {
			continue
		}
		out = append(out, SkillEntry{Name: name, Path: rel})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// discoverMCP 读取根 mcp.json 的 mcpServers 名称（结构校验不在此，加载阶段仅登记）。
func discoverMCP(root *os.Root) []string {
	data, err := root.ReadFile("mcp.json")
	if err != nil {
		return nil // 无 mcp.json → 无 MCP 组件
	}
	var cfg struct {
		MCP serversObject `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	out := make([]string, 0, len(cfg.MCP))
	for name := range cfg.MCP {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type serversObject map[string]any

// Disable 关闭世界书（切回默认模式）。
func (p *Profile) Disable() {
	p.Enabled = false
}
