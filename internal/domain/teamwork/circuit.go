package teamwork

import (
	"sync"
	"time"
)

// CircuitBreaker 外部熔断器（换团队机制）。
// 设计：不靠团队自评（业界证实 AI 不会承认无能），靠独立外部监控客观判断。
// 触发条件（任一命中即熔断）：
//   - 连续失败 ≥3 次
//   - 死循环 ≥3 次
//   - 产出未过质检连续 3 轮
//   - 整体超时 2h
//   - 同团队 ≥2 成员信任降至 L2（警告期）以下
type CircuitBreaker struct {
	mu sync.Mutex

	// ConsecutiveFailures 连续失败计数。
	ConsecutiveFailures int
	// InfiniteLoops 死循环计数。
	InfiniteLoops int
	// QAFailRounds 质检未过连续轮次。
	QAFailRounds int

	// StartedAt 任务开始时间（超时判断）。
	StartedAt time.Time
	// Timeout 整体超时（默认 2h）。
	Timeout time.Duration

	// DemotedMembers 已降级成员数（降级至 L2 以下）。
	DemotedMembers int

	// Tripped 是否已熔断。
	Tripped bool
	// TripReason 熔断原因。
	TripReason string
}

// CircuitConfig 熔断器配置。
type CircuitConfig struct {
	Timeout time.Duration // 整体超时（0=默认 2h）
}

// NewCircuitBreaker 创建熔断器。
func NewCircuitBreaker(cfg CircuitConfig) *CircuitBreaker {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	return &CircuitBreaker{
		StartedAt: time.Now(),
		Timeout:   timeout,
	}
}

// RecordFailure 记录连续失败。
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.ConsecutiveFailures++
	if cb.ConsecutiveFailures >= 3 {
		cb.tripLocked("连续失败 3 次")
	}
}

// RecordSuccess 记录成功（重置连续失败计数）。
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.ConsecutiveFailures = 0
	cb.QAFailRounds = 0
}

// RecordLoop 记录死循环。
func (cb *CircuitBreaker) RecordLoop() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.InfiniteLoops++
	if cb.InfiniteLoops >= 3 {
		cb.tripLocked("死循环 3 次")
	}
}

// RecordQAFail 记录质检未过（连续轮次 +1）。
func (cb *CircuitBreaker) RecordQAFail() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.QAFailRounds++
	if cb.QAFailRounds >= 3 {
		cb.tripLocked("产出未过质检连续 3 轮")
	}
}

// RecordDemotion 记录成员降级至 L2 以下。
func (cb *CircuitBreaker) RecordDemotion() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.DemotedMembers++
	if cb.DemotedMembers >= 2 {
		cb.tripLocked("同团队 2 名成员信任降至警告期以下")
	}
}

// CheckTimeout 检查整体超时（返回是否超时并熔断）。
func (cb *CircuitBreaker) CheckTimeout() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if time.Since(cb.StartedAt) > cb.Timeout {
		cb.tripLocked("整体超时 " + cb.Timeout.String())
		return true
	}
	return false
}

// IsTripped 是否已熔断。
func (cb *CircuitBreaker) IsTripped() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.Tripped
}

// Reason 熔断原因。
func (cb *CircuitBreaker) Reason() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.TripReason
}

// Reset 重置熔断器（换团队后重新开始）。
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.ConsecutiveFailures = 0
	cb.InfiniteLoops = 0
	cb.QAFailRounds = 0
	cb.DemotedMembers = 0
	cb.Tripped = false
	cb.TripReason = ""
	cb.StartedAt = time.Now()
}

// tripLocked 触发熔断（调用方需持锁）。
func (cb *CircuitBreaker) tripLocked(reason string) {
	if cb.Tripped {
		return
	}
	cb.Tripped = true
	cb.TripReason = reason
}

// TripAction 熔断后的处理选项（汇报用户）。
type TripAction int

const (
	// ActionReinforce 补资源（加人/加时间）。
	ActionReinforce TripAction = iota + 1
	// ActionSwitchTeam 换团队。
	ActionSwitchTeam
	// ActionAbort 中止任务。
	ActionAbort
)

// String 处理选项名。
func (a TripAction) String() string {
	switch a {
	case ActionReinforce:
		return "补资源"
	case ActionSwitchTeam:
		return "换团队"
	case ActionAbort:
		return "中止"
	default:
		return "未知"
	}
}
