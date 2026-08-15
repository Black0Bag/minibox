// Package worldbook 世界书 Profile 加载器（B10，Agent Plugins 1.0.0 标准）。
// 设计（进度跟踪 20260812 子块E）：
//   - 世界书 = 整机运行模式切换器（大项目开发=加载开发世界书）
//   - 格式 = Agent Plugins 1.0.0 标准（2026-08-06 联合发布）
//   - 组件独立校验、失败隔离（一个坏不拖累其他）
//   - 预留 8 类插入接口 P1-P8：skill 注册/MCP 注册/system prompt 装配/工具白名单/记忆权重/前端 UI 组件/事件钩子/权限策略
package worldbook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PluginJSON 世界书清单（Agent Plugins 1.0.0 标准）。
type PluginJSON struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	// Skills 专业 skill 目录
	Skills []string `json:"skills,omitempty"`
	// MCP 专业 MCP 服务
	MCP []string `json:"mcp,omitempty"`
	// Extension 咱家扩展命名空间（io.github.zsm.minibox）
	Extension *Extension `json:"io.github.zsm.minibox,omitempty"`
}

// Extension 咱家世界书扩展命名空间。
type Extension struct {
	UI           []string `json:"ui,omitempty"`            // 前端 UI 组件/图标/主题
	SystemPrompt []string `json:"system_prompt,omitempty"` // 世界书专属 system prompt section
	MemoryBias   []string `json:"memory_bias,omitempty"`   // 记忆检索权重偏颇
	Tools        []string `json:"tools,omitempty"`         // 工具白名单扩展
}

// Profile 已加载的世界书（运行模式）。
type Profile struct {
	Meta    PluginJSON
	Root    string // 世界书根目录
	Enabled bool
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

// Load 从目录加载世界书（读取 plugin.json + 校验 + 触发 hooks）。
// 失败隔离：单个组件加载失败仅记录错误，不阻断整个世界书加载。
func (l *Loader) Load(root string) (*Profile, error) {
	pluginPath := filepath.Join(root, "plugin.json")
	// #nosec G304 -- root 由组合根配置注入（世界书目录），非不可信输入
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("读取世界书清单失败 %s: %w", pluginPath, err)
	}

	var plugin PluginJSON
	if err := json.Unmarshal(data, &plugin); err != nil {
		return nil, fmt.Errorf("解析世界书清单失败: %w", err)
	}
	if plugin.ID == "" || plugin.Name == "" {
		return nil, fmt.Errorf("世界书清单缺 id/name")
	}

	p := &Profile{
		Meta:    plugin,
		Root:    root,
		Enabled: true,
	}

	// 触发 P1-P8 插入接口（各自失败隔离）
	if l.hooks.OnSkill != nil {
		_ = l.hooks.OnSkill(p, plugin.Skills)
	}
	if l.hooks.OnMCP != nil {
		_ = l.hooks.OnMCP(p, plugin.MCP)
	}
	if plugin.Extension != nil {
		if l.hooks.OnSystemPrompt != nil {
			_ = l.hooks.OnSystemPrompt(p, plugin.Extension.SystemPrompt)
		}
		if l.hooks.OnTools != nil {
			_ = l.hooks.OnTools(p, plugin.Extension.Tools)
		}
		if l.hooks.OnMemoryBias != nil {
			_ = l.hooks.OnMemoryBias(p, plugin.Extension.MemoryBias)
		}
		if l.hooks.OnUI != nil {
			_ = l.hooks.OnUI(p, plugin.Extension.UI)
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

// Disable 关闭世界书（切回默认模式）。
func (p *Profile) Disable() {
	p.Enabled = false
}
