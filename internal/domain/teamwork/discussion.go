package teamwork

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Discussion 团队讨论（讨论协议 IPP 模式）。
// 设计（4a-4f）：
//   - 4a IPP 模式：成员独立提案（Independent Proposal）→ 批评阶段不强求共识 → 组长汇总定夺
//   - 4b 组长点名制：禁自由抢答，组长点名才发言
//   - 4c 最多 5 轮（组长可提前停）
//   - 4d 分歧组长裁决（涉及加人/危险操作才上报用户）
//   - 4e 依赖检测并行/串行
//   - 4f 问题锁：用户原始需求是不可变基准，每轮结束对照防偏移
type Discussion struct {
	mu sync.Mutex

	// Question 问题锁：用户原始需求（不可变基准）。
	Question string

	// LeadID 组长 ID。
	LeadID string

	// Round 当前轮次。
	Round int

	// MaxRounds 轮次上限（默认 5）。
	MaxRounds int

	// Proposals 各轮提案（key=轮次）。
	Proposals map[int][]Proposal

	// Verdict 组长最终裁决（定稿后非空）。
	Verdict string

	// Done 是否已结束。
	Done bool

	// StartedAt 开始时间。
	StartedAt time.Time
}

// Proposal 成员提案。
type Proposal struct {
	Round    int    `json:"round"`
	MemberID string `json:"member_id"`
	Content  string `json:"content"`
	// Critique 批评阶段：其他成员对提案的批评（不强求共识）。
	Critiques []Critique `json:"critiques,omitempty"`
}

// Critique 批评意见。
type Critique struct {
	MemberID string `json:"member_id"`
	Content  string `json:"content"`
}

// NewDiscussion 创建讨论（问题锁 + 组长 + 轮次上限）。
func NewDiscussion(question, leadID string) *Discussion {
	return &Discussion{
		Question:  strings.TrimSpace(question),
		LeadID:    leadID,
		Round:     0,
		MaxRounds: 5,
		Proposals: make(map[int][]Proposal),
		StartedAt: time.Now(),
	}
}

// SubmitProposal 提交提案（当前轮）。
// 4b 组长点名制：仅被点名的成员可提交（由调用方保证点名顺序）。
func (d *Discussion) SubmitProposal(memberID, content string) (Proposal, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Done {
		return Proposal{}, fmt.Errorf("讨论已结束")
	}
	if d.Round == 0 {
		d.Round = 1
	}
	if d.Round > d.MaxRounds {
		return Proposal{}, fmt.Errorf("超过轮次上限 %d", d.MaxRounds)
	}
	p := Proposal{Round: d.Round, MemberID: memberID, Content: content}
	d.Proposals[d.Round] = append(d.Proposals[d.Round], p)
	return p, nil
}

// AddCritique 对某提案添加批评（4a 批评阶段）。
func (d *Discussion) AddCritique(round int, proposalIdx int, memberID, content string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	ps, ok := d.Proposals[round]
	if !ok || proposalIdx >= len(ps) {
		return fmt.Errorf("提案不存在: 轮%d 序号%d", round, proposalIdx)
	}
	ps[proposalIdx].Critiques = append(ps[proposalIdx].Critiques, Critique{MemberID: memberID, Content: content})
	d.Proposals[round] = ps
	return nil
}

// NextRound 进入下一轮（组长判定需继续）。
// 返回 false 表示已达轮次上限，必须裁决。
func (d *Discussion) NextRound() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Done {
		return false
	}
	if d.Round >= d.MaxRounds {
		return false
	}
	d.Round++
	return true
}

// Conclude 组长裁决（4d 分歧组长裁决），结束讨论。
func (d *Discussion) Conclude(verdict string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Done {
		return fmt.Errorf("讨论已结束")
	}
	d.Verdict = strings.TrimSpace(verdict)
	d.Done = true
	return nil
}

// DriftCheck 问题锁（4f）：检查最终结论是否偏离用户原始需求。
// 返回偏移点（空=未偏移）。简化实现：比较裁决对问题实义字符的覆盖率。
func (d *Discussion) DriftCheck() string {
	if d.Verdict == "" {
		return "未裁决"
	}
	core := normalizeQuery(d.Question)
	if core == "" {
		return ""
	}
	overlap := 0
	for _, r := range core {
		if strings.ContainsRune(d.Verdict, r) {
			overlap++
		}
	}
	ratio := float64(overlap) / float64(len([]rune(core)))
	if ratio < 0.5 {
		return fmt.Sprintf("结论偏离需求（字符覆盖率 %.0f%%）", ratio*100)
	}
	return ""
}

// normalizeQuery 归一化问题：去除停用字与空白，返回实义字符序列。
func normalizeQuery(q string) string {
	stop := map[rune]bool{
		'我': true, '你': true, '他': true, '她': true, '它': true, '们': true,
		'的': true, '了': true, '是': true, '在': true, '有': true, '和': true,
		'与': true, '或': true, '请': true, '帮': true, '给': true, '让': true,
		'要': true, '想': true, '这': true, '那': true, '个': true, '一': true,
		'写': true, '做': true, '搞': true, '弄': true, '下': true, '上': true,
	}
	var sb strings.Builder
	for _, r := range q {
		if stop[r] || !isHan(r) {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func isHan(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// DispatchPlan 依赖检测（4e）：根据任务依赖决定串行/并行。
// 有依赖或共享资源 → 串行；无依赖 → 可并行。
type DispatchPlan struct {
	// Groups 分组，同组内串行，组间可并行。
	Groups [][]string `json:"groups"`
}

// TaskDependency 任务依赖声明。
type TaskDependency struct {
	// ID 任务 ID。
	ID string
	// DependsOn 依赖的任务 ID 列表（空=无依赖）。
	DependsOn []string
}

// PlanDispatch 依赖检测调度（轻量 DAG，不引完整 DAG 库）。
// 结果按拓扑分层：每层内任务无依赖可并行，层间串行。
func PlanDispatch(tasks []TaskDependency) (DispatchPlan, error) {
	// 构建入度表 + 依赖图。
	indeg := make(map[string]int)
	rev := make(map[string][]string) // 被依赖者 → 依赖者
	for _, t := range tasks {
		if _, ok := indeg[t.ID]; !ok {
			indeg[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			indeg[t.ID]++
			rev[dep] = append(rev[dep], t.ID)
		}
	}
	// 校验依赖存在性。
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := indeg[dep]; !ok {
				return DispatchPlan{}, fmt.Errorf("依赖不存在: %s -> %s", t.ID, dep)
			}
		}
	}

	// Kahn 拓扑分层。
	var plan DispatchPlan
	remaining := len(indeg)
	for remaining > 0 {
		var layer []string
		for id, d := range indeg {
			if d == 0 {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			return DispatchPlan{}, fmt.Errorf("检测到循环依赖")
		}
		// 层内排序保证稳定输出。
		sort.Strings(layer)
		plan.Groups = append(plan.Groups, layer)
		for _, id := range layer {
			for _, dependent := range rev[id] {
				indeg[dependent]--
			}
			delete(indeg, id)
			remaining--
		}
	}
	return plan, nil
}
