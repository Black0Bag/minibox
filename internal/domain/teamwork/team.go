package teamwork

import (
	"sort"
	"strings"
)

// Team 团队（常设团队预设）。
type Team struct {
	// ID 团队唯一标识（如 "engineering"）。
	ID string `json:"id"`
	// Name 团队名（如「编程团队」）。
	Name string `json:"name"`
	// Description 团队职责描述（前台分诊用）。
	Description string `json:"description"`
	// Keywords 匹配关键词（前台分诊用，需求命中即推荐）。
	Keywords []string `json:"keywords"`
	// LeadRole 组长角色卡。
	LeadRole RoleCard `json:"lead_role"`
	// MemberRoles 成员角色卡（不含组长）。
	MemberRoles []RoleCard `json:"member_roles"`
}

// Member 团队运行时成员实例。
// 组长/成员统一用 Member 表示，通过 Level 区分层级。
type Member struct {
	// ID 成员实例 ID（全局唯一，runtime 生成）。
	ID string `json:"id"`
	// Role 角色卡（角色定义 + 工具授权）。
	Role RoleCard `json:"role"`
	// Level 层级（组长=2，成员=3，子员工=4，终端工人=5）。
	Level Level `json:"level"`
	// Model 独立模型（空=继承主 Agent）。
	Model string `json:"model,omitempty"`
	// Profile 信任档案（7 维评分）。
	Profile *Profile `json:"profile,omitempty"`
	// Permanent 是否常设（永久入职 vs 临时组队）。
	Permanent bool `json:"permanent"`
}

// NewMember 创建成员实例。
func NewMember(id string, role RoleCard, level Level) *Member {
	return &Member{
		ID:      id,
		Role:    role,
		Level:   level,
		Profile: NewProfile(id),
	}
}

// TeamCatalog 常设团队目录（8 个预设）。
type TeamCatalog struct {
	teams map[string]Team
}

// NewTeamCatalog 构建 8 个常设团队目录。
// 设计：8 个常设团队 + 角色卡市场兜底 + 现场组队转常设需用户确认。
func NewTeamCatalog() *TeamCatalog {
	c := &TeamCatalog{teams: make(map[string]Team)}
	for _, t := range presetTeams() {
		c.teams[t.ID] = t
	}
	return c
}

// Get 按 ID 取团队。
func (c *TeamCatalog) Get(id string) (Team, bool) {
	t, ok := c.teams[id]
	return t, ok
}

// All 返回全部团队（按 ID 排序，稳定输出）。
func (c *TeamCatalog) All() []Team {
	out := make([]Team, 0, len(c.teams))
	for _, t := range c.teams {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Match 前台分诊：按需求关键词推荐团队。
// 返回命中团队列表（按匹配得分降序）。空需求返回空。
func (c *TeamCatalog) Match(need string) []Team {
	need = strings.ToLower(strings.TrimSpace(need))
	if need == "" {
		return nil
	}
	type scored struct {
		team  Team
		score int
	}
	var hits []scored
	for _, t := range c.teams {
		score := 0
		for _, kw := range t.Keywords {
			if strings.Contains(need, strings.ToLower(kw)) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{t, score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].team.ID < hits[j].team.ID
	})
	out := make([]Team, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.team)
	}
	return out
}

// presetTeams 8 个常设团队预设。
// 组长 + 成员（角色卡精简为四模块：核心/约束/工具/输出）。
func presetTeams() []Team {
	return []Team{
		{
			ID:   "engineering",
			Name: "编程团队",
			Description: "软件开发、代码编写、调试、架构设计",
			Keywords:    []string{"编程", "代码", "开发", "程序", "bug", "调试", "架构", "软件"},
			LeadRole: NewRoleCard("lead_engineering", "技术组长",
				"技术团队的负责人，负责任务拆解、技术方案裁决、进度协调。",
				"不得直接编写业务代码（专注协调与裁决）；不得绕过用户修改需求。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出任务拆解清单、技术方案、裁决意见。"),
			MemberRoles: []RoleCard{
				NewRoleCard("architect", "架构师",
					"设计系统架构、模块划分、接口定义。",
					"不得编写实现代码；架构变更需组长确认。",
					[]string{"read_knowledge"},
					"输出架构设计文档、接口规范。"),
				NewRoleCard("coder", "代码工程师",
					"编写和实现具体功能代码。",
					"严格按架构师接口实现；不得擅自改接口。",
					[]string{"write_code", "run_test", "read_knowledge"},
					"输出可运行代码 + 测试结果。"),
				NewRoleCard("debugger", "调试员",
					"定位和修复缺陷。",
					"只改缺陷相关代码；不得重构无关代码。",
					[]string{"read_code", "run_test", "write_code"},
					"输出缺陷定位 + 修复 + 回归测试结果。"),
			},
		},
		{
			ID:   "writing",
			Name: "创作团队",
			Description: "文案创作、内容撰写、审校",
			Keywords:    []string{"文案", "创作", "文章", "内容", "校对", "审校", "编辑", "稿件"},
			LeadRole: NewRoleCard("lead_writing", "主编",
				"创作任务统筹、选题定调、质量把关。",
				"不得亲自撰写主体内容；不得改变用户主题。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出选题、大纲、终审意见。"),
			MemberRoles: []RoleCard{
				NewRoleCard("researcher_writer", "资料搜集员",
					"搜集、整理创作所需素材。",
					"只搜不写；素材需注明来源。",
					[]string{"search", "read_knowledge"},
					"输出带来源的素材清单。"),
				NewRoleCard("writer", "撰稿人",
					"按大纲撰写正文。",
					"遵循主编大纲；不得偏离主题。",
					[]string{"write", "read_knowledge"},
					"输出正文初稿。"),
				NewRoleCard("proofreader", "审校员",
					"校对错别字、语法、逻辑。",
					"只改不改写；重大改动需回报主编。",
					[]string{"read_knowledge"},
					"输出审校意见 + 修订稿。"),
			},
		},
		{
			ID:   "office",
			Name: "办公团队",
			Description: "文档整理、表格处理、汇报撰写",
			Keywords:    []string{"文档", "表格", "办公", "汇报", "整理", "ppt", "excel", "word", "会议纪要"},
			LeadRole: NewRoleCard("lead_office", "项目协调员",
				"办公任务统筹、进度跟踪、交付把关。",
				"不得代做具体文档；需确保交付符合用户格式要求。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出任务协调清单、交付确认。"),
			MemberRoles: []RoleCard{
				NewRoleCard("doc_organizer", "文档整理员",
					"整理、归类、格式化文档。",
					"不改变文档原意；保留原始数据。",
					[]string{"read_knowledge", "write"},
					"输出整理后的文档。"),
				NewRoleCard("spreadsheet", "表格处理员",
					"处理表格数据、公式、图表。",
					"数据不得臆造；公式需可复算。",
					[]string{"write", "read_knowledge"},
					"输出处理后的表格。"),
				NewRoleCard("report_writer", "汇报撰写员",
					"撰写汇报材料、摘要、总结。",
					"基于事实，不得夸大；结论需有依据。",
					[]string{"write", "read_knowledge"},
					"输出汇报文档。"),
			},
		},
		{
			ID:   "life",
			Name: "生活团队",
			Description: "生活建议、信息查证、资源推荐",
			Keywords:    []string{"生活", "旅游", "健康", "饮食", "购物", "推荐", "建议", "生活指南"},
			LeadRole: NewRoleCard("lead_life", "生活顾问",
				"生活需求理解、建议定调、资源把关。",
				"不得提供医疗诊断等专业建议（需声明边界）。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出生活建议、资源推荐。"),
			MemberRoles: []RoleCard{
				NewRoleCard("fact_checker", "信息查证员",
					"查证信息的真实性、时效性。",
					"信息需可溯源；不得传谣。",
					[]string{"search", "read_knowledge"},
					"输出查证结果 + 来源。"),
				NewRoleCard("recommender", "资源推荐员",
					"推荐相关资源、商品、方案。",
					"推荐需客观；利益相关需披露。",
					[]string{"search", "read_knowledge"},
					"输出推荐清单 + 理由。"),
			},
		},
		{
			ID:   "research",
			Name: "研究分析团队",
			Description: "市场调研、数据分析、研究报告",
			Keywords:    []string{"研究", "分析", "调研", "报告", "数据", "市场", "趋势", "论文"},
			LeadRole: NewRoleCard("lead_research", "研究主管",
				"研究课题定题、方法论把关、报告定稿。",
				"不得编造数据；结论需有数据支撑。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出研究框架、终审报告。"),
			MemberRoles: []RoleCard{
				NewRoleCard("surveyor", "资料调研员",
					"搜集研究所需资料、数据。",
					"资料需注明来源；数据需可复现。",
					[]string{"search", "read_knowledge"},
					"输出调研资料 + 数据。"),
				NewRoleCard("data_analyst", "数据分析师",
					"分析数据、提炼洞察。",
					"分析需有方法论；不得过度解读。",
					[]string{"read_knowledge"},
					"输出分析结论 + 图表。"),
				NewRoleCard("report_author", "报告撰写员",
					"撰写研究报告正文。",
					"遵循研究主管框架；结论需引用分析师结果。",
					[]string{"write", "read_knowledge"},
					"输出研究报告。"),
			},
		},
		{
			ID:   "data",
			Name: "数据处理团队",
			Description: "数据清洗、统计分析、可视化",
			Keywords:    []string{"数据", "清洗", "统计", "可视化", "图表", "分析", "处理"},
			LeadRole: NewRoleCard("lead_data", "数据主管",
				"数据任务统筹、口径定义、结果把关。",
				"不得篡改数据；口径变更需用户确认。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出数据口径、交付确认。"),
			MemberRoles: []RoleCard{
				NewRoleCard("data_cleaner", "数据清理员",
					"清洗、去重、标准化数据。",
					"清洗规则需记录；不得丢弃原始数据。",
					[]string{"write", "read_knowledge"},
					"输出清洗后数据 + 清洗规则。"),
				NewRoleCard("statistician", "统计分析员",
					"统计建模、假设检验。",
					"方法需可复现；不得选择性报告。",
					[]string{"read_knowledge"},
					"输出统计结果 + 方法说明。"),
				NewRoleCard("visualizer", "可视化专员",
					"制作数据可视化图表。",
					"图表不得误导；坐标轴需标注。",
					[]string{"write", "read_knowledge"},
					"输出图表 + 说明。"),
			},
		},
		{
			ID:   "media",
			Name: "媒体制作团队",
			Description: "图片、视频、音频等媒体制作",
			Keywords:    []string{"图片", "视频", "音频", "媒体", "制作", "设计", "剪辑", "海报"},
			LeadRole: NewRoleCard("lead_media", "制作总监",
				"媒体项目统筹、创意定调、质量把控。",
				"不得侵犯版权；素材需合规。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出制作方案、终审意见。"),
			MemberRoles: []RoleCard{
				NewRoleCard("material_collector", "素材采集员",
					"采集制作所需素材。",
					"素材需合规可用；不得采集侵权素材。",
					[]string{"search", "read_knowledge"},
					"输出素材清单 + 授权说明。"),
				NewRoleCard("media_editor", "媒体编辑员",
					"剪辑、合成媒体内容。",
					"遵循制作总监创意；不得擅自加料。",
					[]string{"write", "read_knowledge"},
					"输出编辑后的媒体内容。"),
				NewRoleCard("qa_reviewer", "质量审核员",
					"审核媒体成品质量、合规性。",
					"发现问题需回报；不得私自重制。",
					[]string{"read_knowledge"},
					"输出审核意见。"),
			},
		},
		{
			ID:   "automation",
			Name: "自动化团队",
			Description: "流程自动化、脚本编写、测试验证",
			Keywords:    []string{"自动化", "脚本", "流程", "批量", "定时", "机器人", "工作流"},
			LeadRole: NewRoleCard("lead_automation", "自动化架构师",
				"自动化方案设计、流程编排、稳定性把关。",
				"不得设计无逃生通道的自动化；需可中断。",
				[]string{"spawn_subagent", "read_knowledge"},
				"输出自动化方案、流程设计。"),
			MemberRoles: []RoleCard{
				NewRoleCard("flow_analyst", "流程分析师",
					"分析现有流程、识别自动化点。",
					"需理解现有流程再自动化；不得臆测。",
					[]string{"read_knowledge"},
					"输出流程分析报告。"),
				NewRoleCard("scripter", "脚本编写员",
					"编写自动化脚本。",
					"脚本需幂等可重跑；需有错误处理。",
					[]string{"write_code", "run_test", "read_knowledge"},
					"输出脚本 + 使用说明。"),
				NewRoleCard("tester", "测试验证员",
					"验证自动化流程的正确性、健壮性。",
					"需覆盖边界情况；失败需回报。",
					[]string{"run_test", "read_knowledge"},
					"输出测试报告。"),
			},
		},
	}
}
