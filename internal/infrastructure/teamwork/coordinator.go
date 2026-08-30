// Package teamwork 团队协作编排引擎（T 系列）。
// 作用：把 domain/teamwork 的数据模型（Scheduler/Discussion/HR/Trust）串成可运行流程：
//
//	前台分诊 → 组建团队 → 成员讨论（LLM 驱动 IPP 协议）→ 组长裁决 → HR 审批 → 结案。
//
// 分层：本包在 infrastructure 层，import domain/teamwork 数据模型 + domain/llm 接口，
// 不 import 平台/传输/存储；具体 LLM 供应商由 app 层经 llm.Provider 注入。
package teamwork

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/llm"
	"github.com/Black0Bag/minibox/internal/domain/teamwork"
)

// LLM 驱动讨论的最小接口（收敛：Coordinator 只依赖 Complete 非流式）。
// app 层注入 llm.Provider（Router），满足分层（本包不碰具体供应商）。
type LLM interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Coordinator 团队协作编排中枢。
// 组合 domain 数据模型 + LLM 生成器，对外暴露端到端编排方法。
type Coordinator struct {
	mu  sync.Mutex
	log *slog.Logger

	sched *teamwork.Scheduler
	llm   LLM

	// 运行中的项目（sessionID → 项目会话）。轻量内存态；持久化留后续 Phase。
	projects map[string]*Project
}

// Project 单个项目模式会话。
type Project struct {
	// ID 项目会话 ID（sessionID 同源，全局唯一）。
	ID string
	// Question 用户原始需求（问题锁，不可变基准）。
	Question string
	// Team 已选团队。
	Team teamwork.Team
	// Members 已实例化成员（组长 + 成员）。
	Members []teamwork.Member
	// Discussion 当前讨论（可为 nil，未进入讨论阶段）。
	Discussion *teamwork.Discussion
	// Verdict 组长最终裁决（定稿后非空）。
	Verdict string
	// Status 阶段状态：triage→confirmed→discussing→planning→executing→done。
	Status string
	// CreatedAt 创建时间。
	CreatedAt time.Time
}

// NewCoordinator 创建编排中枢。
// catalog 为 nil 时用默认 8 团队；hr 为 nil 时新建空 HR。
func NewCoordinator(log *slog.Logger, sched *teamwork.Scheduler, llm LLM) *Coordinator {
	if sched == nil {
		sched = teamwork.NewScheduler(nil, nil)
	}
	return &Coordinator{
		log:      log,
		sched:    sched,
		llm:      llm,
		projects: make(map[string]*Project),
	}
}

// Scheduler 返回底层调度中枢（测试/扩展用）。
func (c *Coordinator) Scheduler() *teamwork.Scheduler { return c.sched }

// Project 返回指定项目（不存在返回 nil,false）。
func (c *Coordinator) Project(id string) (*Project, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.projects[id]
	return p, ok
}

// Triage 前台分诊：只推荐团队，不执行（对接项目模式步骤 1）。
func (c *Coordinator) Triage(ctx context.Context, need string) (*teamwork.Triage, error) {
	return c.sched.TriageTask(ctx, need)
}

// StartProject 确认团队后启动项目（对接步骤 2-3）：创建讨论 + 实例化成员。
// tasks 可为 nil（无依赖声明的简单项目）；teamID 空时取分诊主推团队。
func (c *Coordinator) StartProject(ctx context.Context, projectID, question, teamID string, tasks []teamwork.TaskDependency) (*Project, error) {
	if strings.TrimSpace(question) == "" {
		return nil, fmt.Errorf("项目问题为空")
	}
	if projectID == "" {
		return nil, fmt.Errorf("项目 ID 为空")
	}

	var team teamwork.Team
	if teamID != "" {
		t, ok := c.sched.Catalog.Get(teamID)
		if !ok {
			return nil, fmt.Errorf("团队不存在: %s", teamID)
		}
		team = t
	} else {
		// 未指定团队：分诊主推第一个。
		triage, err := c.sched.TriageTask(ctx, question)
		if err != nil {
			return nil, err
		}
		if len(triage.Recommended) == 0 {
			return nil, fmt.Errorf("无匹配团队")
		}
		team = *triage.Recommended[0].Team
	}

	// 实例化成员：组长 + 各成员。
	members := make([]teamwork.Member, 0, 1+len(team.MemberRoles))
	leadID := "lead_" + team.ID
	members = append(members, *teamwork.NewMember(leadID, team.LeadRole, teamwork.Level2))
	for i, rc := range team.MemberRoles {
		members = append(members, *teamwork.NewMember(fmt.Sprintf("member_%s_%d", team.ID, i), rc, teamwork.Level3))
	}

	out, err := c.sched.StartProject(ctx, question, leadID, tasks)
	if err != nil {
		return nil, err
	}

	p := &Project{
		ID:         projectID,
		Question:   strings.TrimSpace(question),
		Team:       team,
		Members:    members,
		Discussion: out.Discussion,
		Status:     "confirmed",
		CreatedAt:  time.Now(),
	}

	c.mu.Lock()
	c.projects[projectID] = p
	c.mu.Unlock()
	return p, nil
}

// RunDiscussion 驱动一轮成员讨论（IPP 协议，LLM 生成）。
// 每成员按角色卡 core 生成提案；无 LLM 时退回模板占位（仍走流程，LLM 空降后可用真实内容）。
// 返回本轮提案列表。
func (c *Coordinator) RunDiscussion(ctx context.Context, projectID, roundPrompt string) ([]teamwork.Proposal, error) {
	p, ok := c.Project(projectID)
	if !ok {
		return nil, fmt.Errorf("项目不存在: %s", projectID)
	}
	if p.Discussion == nil {
		return nil, fmt.Errorf("项目 %s 未进入讨论阶段（先 StartProject）", projectID)
	}
	if p.Discussion.Done {
		return nil, fmt.Errorf("讨论已结束（先裁决）")
	}

	if p.Status == "confirmed" {
		p.Status = "discussing"
	}

	// 只让成员（非组长）提方案，组长做裁决。
	var proposers []teamwork.Member
	for _, m := range p.Members {
		if m.Level != teamwork.Level2 { // 排除组长
			proposers = append(proposers, m)
		}
	}
	if len(proposers) == 0 {
		return nil, fmt.Errorf("团队无成员可提案（缺成员角色卡）")
	}

	var proposals []teamwork.Proposal
	for _, m := range proposers {
		content, err := c.generateProposal(ctx, m, p.Question, roundPrompt)
		if err != nil {
			// LLM 失败不阻断流程：退回角色模板占位，流程仍可继续
			content = "（LLM 未就绪）" + m.Role.Core + " 待命"
		}
		prop, err := p.Discussion.SubmitProposal(m.ID, content)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, prop)
	}
	return proposals, nil
}

// generateProposal 用 LLM 为某成员生成提案（角色卡 core 做系统提示）。
func (c *Coordinator) generateProposal(ctx context.Context, m teamwork.Member, question, extra string) (string, error) {
	if c.llm == nil {
		return "", fmt.Errorf("LLM 未注入")
	}
	sys := m.Role.Core
	if m.Role.Constraints != "" {
		sys += "\n约束：" + m.Role.Constraints
	}
	user := "项目问题：" + question
	if extra != "" {
		user += "\n本轮补充：" + extra
	}
	user += "\n请以你的专业角色视角，给出对问题的一句话方案提案（不含冗长分析）。"

	resp, err := c.llm.Complete(ctx, llm.Request{
		Model:    "",
		Feature:  llm.FeatureAgent,
		Messages: []llm.Message{{Role: llm.RoleSystem, Content: sys}, {Role: llm.RoleUser, Content: user}},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.Content) == "" {
		return "", fmt.Errorf("LLM 空响应")
	}
	return strings.TrimSpace(resp.Content), nil
}

// Conclude 组长裁决结案（对接步骤 6）：组长汇总各轮提案出定稿，更新项目状态与信任档案。
func (c *Coordinator) Conclude(ctx context.Context, projectID, verdict string) (*Project, error) {
	p, ok := c.Project(projectID)
	if !ok {
		return nil, fmt.Errorf("项目不存在: %s", projectID)
	}
	if p.Discussion == nil {
		return nil, fmt.Errorf("项目 %s 未进入讨论阶段", projectID)
	}
	if strings.TrimSpace(verdict) == "" {
		return nil, fmt.Errorf("组长裁决为空")
	}

	if err := p.Discussion.Conclude(verdict); err != nil {
		return nil, err
	}
	p.Verdict = verdict
	p.Status = "done"

	// 信任档案：本轮参与成员记录一次完成（成功路径）。
	for i := range p.Members {
		if p.Members[i].Profile != nil {
			p.Members[i].Profile.RecordCompletion(true)
		}
	}
	return p, nil
}

// RequestStaffing 人力增援审批（对接自进化：成员发现人手不足 → 上报）。
// 委托 domain HR 三层审批；返回决策 + 预警等级。
func (c *Coordinator) RequestStaffing(ctx context.Context, req teamwork.StaffingRequest) ([]teamwork.StaffingDecision, teamwork.StaffingAlarm, error) {
	return c.sched.HR.Review(req)
}
