package teamwork

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// DispatchOutcome 调度结果。
type DispatchOutcome struct {
	// Triage 前台分诊推荐。
	Triage *Triage
	// Plan 依赖检测调度计划（ModeProject 时非空）。
	Plan *DispatchPlan
	// Discussion 讨论（有组长时）。
	Discussion *Discussion
}

// Triage 前台分诊结果（方案 B：推荐后确认）。
// 需求明确 → 推荐 1 团队 + 理由；需求模糊 → 2-3 备选。
type Triage struct {
	// Recommended 推荐团队（需求明确时单数，模糊时第一个为主推）。
	Recommended []TeamRecommendation
	// NeedClarify 是否需要用户澄清（需求模糊）。
	NeedClarify bool
}

// TeamRecommendation 团队推荐。
type TeamRecommendation struct {
	Team   *Team
	Reason string
}

// Scheduler 团队调度中枢。
// 职责：前台分诊（只分诊不干活）、触发讨论协议、依赖检测、串联 HR 与熔断。
type Scheduler struct {
	mu sync.Mutex

	// Catalog 团队目录（8 常设团队）。
	Catalog *TeamCatalog
	// HR HR 审批助手。
	HR *HRAssistant
}

// NewScheduler 创建调度中枢。
func NewScheduler(catalog *TeamCatalog, hr *HRAssistant) *Scheduler {
	if catalog == nil {
		catalog = presetTeams()
	}
	if hr == nil {
		hr = NewHRAssistant(nil)
	}
	return &Scheduler{Catalog: catalog, HR: hr}
}

// TriageTask 前台分诊（只分诊，不执行）。
// 设计：需求明确 → 推荐 1 团队 + 简短理由；需求模糊 → 2-3 备选 + 各附理由。
func (s *Scheduler) TriageTask(ctx context.Context, need string) (*Triage, error) {
	if need == "" {
		return nil, fmt.Errorf("需求为空")
	}
	matches := s.Catalog.Match(need)
	if len(matches) == 0 {
		return nil, fmt.Errorf("无匹配团队，需现场组队")
	}

	// 重新计算得分以判断需求明确度。
	scores := make([]int, len(matches))
	for i, team := range matches {
		scores[i] = teamKeywordScore(team, need)
	}

	t := &Triage{}
	// 首个匹配分显著高于后续 → 需求明确。
	if len(matches) == 1 || scores[0] >= scores[1]*2 {
		t.Recommended = []TeamRecommendation{{Team: &matches[0], Reason: "需求关键词高度匹配"}}
		t.NeedClarify = false
	} else {
		// 需求模糊 → 取前 2-3 备选。
		n := len(matches)
		if n > 3 {
			n = 3
		}
		for i := 0; i < n; i++ {
			t.Recommended = append(t.Recommended, TeamRecommendation{
				Team:   &matches[i],
				Reason: "候选团队，待用户确认",
			})
		}
		t.NeedClarify = true
	}
	return t, nil
}

// teamKeywordScore 计算团队关键词命中得分。
func teamKeywordScore(team Team, need string) int {
	need = strings.ToLower(strings.TrimSpace(need))
	score := 0
	for _, kw := range team.Keywords {
		if strings.Contains(need, strings.ToLower(kw)) {
			score++
		}
	}
	return score
}

// StartProject 启动项目模式：创建讨论 + 依赖检测。
func (s *Scheduler) StartProject(ctx context.Context, question string, leadID string, tasks []TaskDependency) (*DispatchOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if question == "" {
		return nil, fmt.Errorf("项目问题为空")
	}

	out := &DispatchOutcome{
		Discussion: NewDiscussion(question, leadID),
	}

	if len(tasks) > 0 {
		plan, err := PlanDispatch(tasks)
		if err != nil {
			return nil, fmt.Errorf("依赖检测失败: %w", err)
		}
		out.Plan = &plan
	}
	return out, nil
}
