// Package degradation 提供四级资源降级（B16：L0-L3 + 迟滞带防抖）。
// 设计：PRD B16，900M 内存路由器 ≤512M 降级验证。
// 状态：L0(全功能) → L1(减上下文) → L2(禁 embedding) → L3(纯文本 fallback)。
// 迟滞带：上升需连续 3 次高负载，下降需连续 5 次低负载（防抖）。
package degradation

import (
	"runtime"
	"sync"
)

// Level 降级等级。
type Level int

const (
	// LevelFull L0：全功能模式。
	LevelFull Level = 0
	// LevelReduced L1：减小上下文窗口。
	LevelReduced Level = 1
	// LevelNoEmbed L2：禁用向量检索。
	LevelNoEmbed Level = 2
	// LevelTextOnly L3：纯文本 fallback。
	LevelTextOnly Level = 3
)

// String 等级可读名。
func (l Level) String() string {
	switch l {
	case LevelFull:
		return "全功能"
	case LevelReduced:
		return "减上下文"
	case LevelNoEmbed:
		return "禁向量"
	case LevelTextOnly:
		return "纯文本"
	default:
		return "未知"
	}
}

// Monitor 资源监控 + 降级决策（迟滞带防抖）。
type Monitor struct {
	mu    sync.Mutex
	level Level

	// 迟滞带计数器
	highCount int // 连续高负载次数
	lowCount  int // 连续低负载次数

	// 阈值
	memHighMB int // 内存高负载阈值（MB），默认 400
	memLowMB  int // 内存低负载阈值（MB），默认 200
	riseCount int // 上升需连续高负载次数，默认 3
	fallCount int // 下降需连续低负载次数，默认 5
}

// NewMonitor 创建降级监控器。
func NewMonitor() *Monitor {
	return &Monitor{
		level:     LevelFull,
		memHighMB: 400,
		memLowMB:  200,
		riseCount: 3,
		fallCount: 5,
	}
}

// Tick 采样一次资源状态，返回是否有降级变化。
func (m *Monitor) Tick() (Level, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	usedMB := int(memStats.Alloc / 1024 / 1024) // #nosec G115 -- 900M 内存路由器 ≤512M，Alloc 远小于 int 上限

	prev := m.level
	if usedMB > m.memHighMB {
		m.highCount++
		m.lowCount = 0
		if m.highCount >= m.riseCount && m.level < LevelTextOnly {
			m.level++
			m.highCount = 0
		}
	} else if usedMB < m.memLowMB {
		m.lowCount++
		m.highCount = 0
		if m.lowCount >= m.fallCount && m.level > LevelFull {
			m.level--
			m.lowCount = 0
		}
	}
	return m.level, m.level != prev
}

// Level 返回当前降级等级。
func (m *Monitor) Level() Level {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.level
}

// ContextBudget 返回当前等级下的上下文 token 预算。
func (m *Monitor) ContextBudget() int {
	switch m.Level() {
	case LevelFull:
		return 128000
	case LevelReduced:
		return 32000
	case LevelNoEmbed:
		return 16000
	case LevelTextOnly:
		return 8000
	default:
		return 8000
	}
}

// EmbedEnabled 当前等级是否启用向量检索。
func (m *Monitor) EmbedEnabled() bool {
	return m.Level() < LevelNoEmbed
}
