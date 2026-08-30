// Package teamwork 团队协作系统（T 系列）。
// 将 Agent 系统组织为「大型服务型公司」：用户=甲方、对话框=前台、专业领域=项目组。
// 设计（源自 dev/系统设计/02_团队协作系统设计.md）：
//   - 三种工作模式（对话 / Plan-Build / 项目），可随时升档/降档
//   - 5 级层级硬上限（Level 1 主 Agent → Level 5 终端工人，物理封死递归）
//   - 8 个常设团队预设 + 角色卡市场兜底 + 现场组队转常设需用户确认
//   - 信任档案 7 维评分 + 按对话次数衰减 + 自动淘汰
//   - 讨论协议 IPP 模式（独立提案 + 批评 + 组长裁决 + 问题锁）
//   - HR 三层审批链路 + 增派预警 + 外部熔断器换团队
package teamwork

// Mode 工作模式。
type Mode string

const (
	// ModeChat 对话模式：一问一答，单 Agent 直接应答。
	ModeChat Mode = "chat"
	// ModePlanBuild Plan-Build 模式：先出方案 → 用户确认 → 再执行。
	ModePlanBuild Mode = "plan_build"
	// ModeProject 项目模式：专业团队接管，前台分诊 → 组建 → 讨论 → 执行。
	ModeProject Mode = "project"
)

// Valid 校验模式是否合法。
func (m Mode) Valid() bool {
	switch m {
	case ModeChat, ModePlanBuild, ModeProject:
		return true
	default:
		return false
	}
}

// Level 层级（5 级硬上限，技术封死）。
type Level int

const (
	// Level1 主 Agent（老板/前台），可派团队。
	Level1 Level = 1
	// Level2 团队组长，可派成员。
	Level2 Level = 2
	// Level3 团队成员，可派子员工（自进化新建）。
	Level3 Level = 3
	// Level4 子员工，可派终端工人。
	Level4 Level = 4
	// Level5 终端工人，工具列表不含「派子代理」，物理封死。
	Level5 Level = 5

	// MaxLevel 层级硬上限（超过直接拒绝递归，防无限嵌套）。
	MaxLevel = Level5
)

// CanSpawn 当前层级能否再生子代理。
// Level 5（终端工人）工具列表不含派子代理，物理封死。
func (l Level) CanSpawn() bool {
	return l < Level5
}

// Valid 校验层级是否在 [1,5] 硬上限内。
func (l Level) Valid() bool {
	return l >= Level1 && l <= MaxLevel
}
