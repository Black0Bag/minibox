package teamwork

import (
	"strings"
)

// RoleCard 角色卡（四模块格式）。
// 设计：角色卡以 Markdown 存知识库，四模块为：
//   1. 角色与核心身份 2. 约束与边界 3. 工具授权 4. 输出规范。
type RoleCard struct {
	ID   string `json:"id"`
	Name string `json:"name"` // 角色名，如「架构师」「调试员」

	// Core 模块一：角色与核心身份（是谁、职责、服务对象）。
	Core string `json:"core"`
	// Constraints 模块二：约束与边界（不能做什么、边界）。
	Constraints string `json:"constraints"`
	// Tools 模块三：工具授权（可用工具白名单）。
	Tools []string `json:"tools"`
	// Output 模块四：输出规范（输出格式、风格、验收标准）。
	Output string `json:"output"`
}

// NewRoleCard 创建角色卡。
func NewRoleCard(id, name, core, constraints string, tools []string, output string) RoleCard {
	return RoleCard{
		ID:          id,
		Name:        name,
		Core:        core,
		Constraints: constraints,
		Tools:       tools,
		Output:      output,
	}
}

// Validate 校验角色卡四模块完整性。
func (rc RoleCard) Validate() error {
	if rc.ID == "" {
		return errMissingField("角色卡 id")
	}
	if rc.Name == "" {
		return errMissingField("角色卡 name")
	}
	if strings.TrimSpace(rc.Core) == "" {
		return errMissingField("角色卡模块一（角色与核心身份）")
	}
	if strings.TrimSpace(rc.Constraints) == "" {
		return errMissingField("角色卡模块二（约束与边界）")
	}
	if strings.TrimSpace(rc.Output) == "" {
		return errMissingField("角色卡模块四（输出规范）")
	}
	return nil
}

// Markdown 序列化为 Markdown（存知识库格式）。
func (rc RoleCard) Markdown() string {
	s := "# 角色卡：" + rc.Name + "\n\n"
	s += "## 一、角色与核心身份\n" + rc.Core + "\n\n"
	s += "## 二、约束与边界\n" + rc.Constraints + "\n\n"
	if len(rc.Tools) > 0 {
		s += "## 三、工具授权\n- " + strings.Join(rc.Tools, "\n- ") + "\n\n"
	}
	s += "## 四、输出规范\n" + rc.Output + "\n"
	return s
}

// MarketEntry 角色卡市场条目（人力资源市场）。
// 市场提供兜底角色卡来源（如 System-Prompt-Library 等）。
type MarketEntry struct {
	Card RoleCard `json:"card"`
	// Source 来源（如 "system-prompt-library" / "local"）。
	Source string `json:"source"`
}
