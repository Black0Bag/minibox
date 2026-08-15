// Package skill 实现 skill 三级渐进披露（Anthropic/Helix/Genkit 实证）。
// 设计（进度跟踪 20260812 子块E）：
//   - Level1：元数据常驻 system（~100 token，只列"做什么+Use when"）
//   - Level2：load_skill 按需加载 body（append-only 不碰缓存）
//   - Level3：read_skill_file 读资源文件
//   - 描述铁律：写做什么 + Use when，不写工作流（Anthropic 实证）
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Metadata skill 元数据（Level1 常驻 system，~100 token）。
// 描述铁律：只写"做什么 + Use when"，不写工作流。
type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description"` // "写做什么 + Use when"
	Version     string `json:"version"`
}

// Skill 一个 skill。
type Skill struct {
	Meta       Metadata
	Root       string // skill 目录
	bodyLoaded bool   // Level2 body 是否已加载（缓存纯追加）
	body       string
}

// Level1 元数据（默认精简，~100 token）。
func (s *Skill) Level1() string {
	return fmt.Sprintf("skill:%s - %s", s.Meta.Name, s.Meta.Description)
}

// LoadBody 按需加载 skill body（Level2，append-only 缓存）。
// 已加载则直接返回缓存（幂等，不重复读盘）。
func (s *Skill) LoadBody() (string, error) {
	if s.bodyLoaded {
		return s.body, nil // 缓存命中（append-only，不覆盖）
	}
	path := filepath.Join(s.Root, "SKILL.md")
	// #nosec G304 -- path 由 skill 目录拼接固定文件名，目录由组合根配置注入
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取 skill body 失败: %w", err)
	}
	s.body = string(data)
	s.bodyLoaded = true
	return s.body, nil
}

// ReadFile 读取 skill 资源文件（Level3，read_skill_file）。
// 路径限制在 skill 目录内（golang-security 路径沙箱）。
func (s *Skill) ReadFile(relPath string) (string, error) {
	// 防路径穿越：只允许 skill 目录内的资源
	clean := filepath.Clean(relPath)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("skill 资源路径越界: %s", relPath)
	}
	full := filepath.Join(s.Root, clean)
	// #nosec G304 -- relPath 已做穿越防护（上一行），目录由组合根配置注入
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("读取 skill 资源失败: %w", err)
	}
	return string(data), nil
}

// Registry skill 注册表（世界书 P1 插入接口挂这里）。
type Registry struct {
	skills map[string]*Skill
	order  []string // 保持注册顺序
}

// NewRegistry 创建 skill 注册表。
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

// Register 注册 skill。
func (r *Registry) Register(s *Skill) error {
	if s == nil || s.Meta.Name == "" {
		return fmt.Errorf("skill 缺名称")
	}
	if _, ok := r.skills[s.Meta.Name]; ok {
		return fmt.Errorf("skill 重名: %s", s.Meta.Name)
	}
	r.skills[s.Meta.Name] = s
	r.order = append(r.order, s.Meta.Name)
	return nil
}

// Get 获取 skill。
func (r *Registry) Get(name string) (*Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// List 列出所有 skill 元数据（Level1 视图，~100 token/条）。
func (r *Registry) List() []Metadata {
	out := make([]Metadata, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.skills[name].Meta)
	}
	return out
}

// Count skill 数量。
func (r *Registry) Count() int {
	return len(r.skills)
}
