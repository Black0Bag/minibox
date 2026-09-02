package teamwork

import (
	"fmt"
	"sync"
	"time"
)

// TrustLevel 信任等级（5 级）。
type TrustLevel int

const (
	// TrustRetired 退役：<1.5，不指派。
	TrustRetired TrustLevel = 1
	// TrustWarn 警告期：1.5-2.5，慎用。
	TrustWarn TrustLevel = 2
	// TrustObserve 观察期：2.5-3.5，可指派需监督。
	TrustObserve TrustLevel = 3
	// TrustRegular 常规：3.5-4.5，正常指派。
	TrustRegular TrustLevel = 4
	// TrustCore 核心骨干：>=4.5，优先指派。
	TrustCore TrustLevel = 5
)

// TrustThreshold 各级别评分阈值。
const (
	trustCoreMin    = 4.5
	trustRegularMin = 3.5
	trustObserveMin = 2.5
	trustWarnMin    = 1.5
)

// Profile 信任档案（7 维评分）。
// 设计：不靠团队自评（业界证实 AI 不会承认无能），靠独立外部监控客观打分。
// REST DTO：字段名为 lower_snake_case（rules.md）。
type Profile struct {
	mu sync.Mutex

	MemberID string `json:"member_id"`

	// SuccessRate 成功率：完成数 ÷ 总分配数。
	SuccessRate float64 `json:"success_rate"`
	// ReasoningConsistency 推理一致性：前后矛盾统计（1=完全一致）。
	ReasoningConsistency float64 `json:"reasoning_consistency"`
	// ToolAccuracy 工具调用准确率：正确数 ÷ 总调用数。
	ToolAccuracy float64 `json:"tool_accuracy"`
	// Compliance 合规度：违规次数折算（1=零违规）。
	Compliance float64 `json:"compliance"`
	// SettlementRate 结算率：正常完成 ÷ (正常 + 崩溃 + 浪费重试)。
	SettlementRate float64 `json:"settlement_rate"`
	// MaintenanceDecay 维护衰减：按主对话次数（非天数）扣减。
	MaintenanceDecay float64 `json:"maintenance_decay"`

	// Completed 已完成任务数。
	Completed int `json:"completed"`
	// Assigned 总分配任务数。
	Assigned int `json:"assigned"`
	// ToolCallsTotal 工具调用总数。
	ToolCallsTotal int `json:"tool_calls_total"`
	// ToolCallsOK 工具调用正确数。
	ToolCallsOK int `json:"tool_calls_ok"`
	// Violations 违规次数。
	Violations int `json:"violations"`
	// SettledNormal 正常结算次数。
	SettledNormal int `json:"settled_normal"`
	// SettledCrash 崩溃结算次数。
	SettledCrash int `json:"settled_crash"`
	// SettledWaste 浪费重试结算次数。
	SettledWaste int `json:"settled_waste"`

	// ConversationsSinceAssign 距上次被指派经过的主对话次数（维护衰减计数）。
	ConversationsSinceAssign int `json:"conversations_since_assign"`

	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// NewProfile 创建信任档案（新成员默认 3.0 观察期）。
func NewProfile(memberID string) *Profile {
	return &Profile{
		MemberID:                 memberID,
		SuccessRate:              3.0,
		ReasoningConsistency:     3.0,
		ToolAccuracy:             3.0,
		Compliance:               3.0,
		SettlementRate:           3.0,
		MaintenanceDecay:         3.0,
		ConversationsSinceAssign: 0,
		UpdatedAt:                time.Now(),
	}
}

// Score 计算综合评分（7 维等权平均）。
func (p *Profile) Score() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	sum := p.SuccessRate + p.ReasoningConsistency + p.ToolAccuracy +
		p.Compliance + p.SettlementRate + p.MaintenanceDecay
	// 反刷分保护：检测异常评分模式（单维突变 >2.0 视为刷分，拉回均值）。
	if p.isAnomalous() {
		sum -= p.SuccessRate
		return sum / 5.0
	}
	return sum / 6.0
}

// Level 根据评分映射信任等级。
func (p *Profile) Level() TrustLevel {
	s := p.Score()
	switch {
	case s >= trustCoreMin:
		return TrustCore
	case s >= trustRegularMin:
		return TrustRegular
	case s >= trustObserveMin:
		return TrustObserve
	case s >= trustWarnMin:
		return TrustWarn
	default:
		return TrustRetired
	}
}

// isAnomalous 反刷分保护：检测单维突变（如一夜之间成功率暴涨）。
func (p *Profile) isAnomalous() bool {
	// 异常模式：成功率为满分但其他维度低（典型刷分特征）。
	return p.SuccessRate >= 5.0 && (p.ToolAccuracy < 2.0 || p.Compliance < 2.0)
}

// RecordCompletion 记录一次任务完成（成功）。
func (p *Profile) RecordCompletion(ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Assigned++
	p.ConversationsSinceAssign = 0
	if ok {
		p.Completed++
		p.SettledNormal++
	} else {
		p.SettledWaste++
	}
	p.recomputeLocked()
}

// RecordToolCall 记录一次工具调用。
func (p *Profile) RecordToolCall(ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ToolCallsTotal++
	if ok {
		p.ToolCallsOK++
	}
	p.recomputeLocked()
}

// RecordViolation 记录一次违规。
func (p *Profile) RecordViolation() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Violations++
	p.recomputeLocked()
}

// RecordInconsistency 记录一次前后矛盾。
func (p *Profile) RecordInconsistency() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ReasoningConsistency = clamp(p.ReasoningConsistency - 0.2)
	p.recomputeLocked()
}

// RecordCrash 记录一次崩溃结算。
func (p *Profile) RecordCrash() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SettledCrash++
	p.recomputeLocked()
}

// Decay 维护衰减：每次主对话未指派该成员扣 0.01（按对话次数非天数）。
func (p *Profile) Decay() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ConversationsSinceAssign++
	p.MaintenanceDecay = clamp(p.MaintenanceDecay - 0.01)
	p.recomputeLocked()
}

// Demote 运行时降级：异常行为立即降权（如发现越权操作）。
func (p *Profile) Demote(amount float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Compliance = clamp(p.Compliance - amount)
	p.SuccessRate = clamp(p.SuccessRate - amount/2)
	p.recomputeLocked()
}

// recomputeLocked 重算派生维度（调用方需持锁）。
func (p *Profile) recomputeLocked() {
	if p.Assigned > 0 {
		p.SuccessRate = float64(p.Completed) / float64(p.Assigned) * 5.0
	}
	if p.ToolCallsTotal > 0 {
		p.ToolAccuracy = float64(p.ToolCallsOK) / float64(p.ToolCallsTotal) * 5.0
	}
	// 合规度：基础 5.0，每违规一次扣 0.5，最低 0。
	p.Compliance = clamp(5.0 - float64(p.Violations)*0.5)
	total := p.SettledNormal + p.SettledCrash + p.SettledWaste
	if total > 0 {
		p.SettlementRate = float64(p.SettledNormal) / float64(total) * 5.0
	}
	p.UpdatedAt = time.Now()
}

// String 人类可读的信任等级。
func (l TrustLevel) String() string {
	switch l {
	case TrustCore:
		return "核心骨干"
	case TrustRegular:
		return "常规"
	case TrustObserve:
		return "观察期"
	case TrustWarn:
		return "警告期"
	case TrustRetired:
		return "退役"
	default:
		return "未知"
	}
}

// clamp 限制评分到 [0,5]。
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 5.0 {
		return 5.0
	}
	return v
}

// ValidateProfile 校验档案合法性（防御性）。
func ValidateProfile(p *Profile) error {
	if p == nil {
		return fmt.Errorf("信任档案为空")
	}
	if p.MemberID == "" {
		return fmt.Errorf("信任档案缺 member_id")
	}
	return nil
}
