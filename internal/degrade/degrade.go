package degrade

import (
	"sync"
	"time"
)

// Level 四级降级等级（N-15）
type Level int

const (
	L0 Level = iota // 正常：全部能力
	L1              // 轻度：限制非关键后台任务
	L2              // 中度：暂停后台/编排任务
	L3              // 严重：仅保留核心 API
)

// Name 级别中文名
func (l Level) Name() string {
	switch l {
	case L0:
		return "正常"
	case L1:
		return "轻度"
	case L2:
		return "中度"
	case L3:
		return "严重"
	}
	return "未知"
}

// Config 降级配置（N-15：迟滞带防抖）
type Config struct {
	TriggerL1   float64       // L1 触发阈值（CPU 或内存 %）
	TriggerL2   float64       // L2 触发阈值
	TriggerL3   float64       // L3 触发阈值
	TriggerHold time.Duration // 触发确认时长（持续超阈值才升级，默认 30s）
	RecoveryHold time.Duration // 恢复确认时长（持续低于阈值才降级，默认 60s）
	Hysteresis  float64       // 迟滞带百分比（恢复需低于阈值 - 迟滞带，防抖）
}

// DefaultConfig 默认配置（对应 N-15：触发 30s / 恢复 60s 低于阈值 5%）
func DefaultConfig() Config {
	return Config{
		TriggerL1:   80,
		TriggerL2:   90,
		TriggerL3:   95,
		TriggerHold: 30 * time.Second,
		RecoveryHold: 60 * time.Second,
		Hysteresis:  5,
	}
}

// ProbeFunc 指标探测函数：返回 CPU%（0-100）、内存%（0-100）
type ProbeFunc func() (cpu, mem float64)

// Degrader 四级降级状态机
type Degrader struct {
	mu    sync.Mutex
	probe ProbeFunc
	cfg   Config

	level Level

	// 当前超阈值起始时间（用于升级确认）
	aboveStart time.Time
	above      bool
	// 当前低于阈值起始时间（用于恢复确认）
	recoverStart time.Time
	recovering   bool
}

// New 创建降级状态机
func New(probe ProbeFunc, cfg Config) *Degrader {
	if cfg.TriggerHold <= 0 {
		cfg.TriggerHold = 30 * time.Second
	}
	if cfg.RecoveryHold <= 0 {
		cfg.RecoveryHold = 60 * time.Second
	}
	return &Degrader{probe: probe, cfg: cfg}
}

// Evaluate 依据当前指标评估降级等级（由监控循环周期性调用）
func (d *Degrader) Evaluate(now time.Time) Level {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.probe == nil {
		return d.level
	}
	cpu, mem := d.probe()
	val := cpu
	if mem > val {
		val = mem
	}

	// 依据当前指标独立计算目标级别（与当前级别无关）
	target := L0
	switch {
	case val >= d.cfg.TriggerL3:
		target = L3
	case val >= d.cfg.TriggerL2:
		target = L2
	case val >= d.cfg.TriggerL1:
		target = L1
	}

	if target > d.level {
		// 升级：需持续 TriggerHold
		if !d.above {
			d.above = true
			d.aboveStart = now
			d.recovering = false
			d.recoverStart = time.Time{}
		}
		if now.Sub(d.aboveStart) >= d.cfg.TriggerHold {
			d.level = target
		}
		return d.level
	}

	// 降级判定：目标低于当前级别，需低于阈值-迟滞带 且持续 RecoveryHold
	if target < d.level {
		threshold := thresholdFor(d.cfg, d.level)
		below := val < threshold-d.cfg.Hysteresis
		if below {
			if !d.recovering {
				d.recovering = true
				d.recoverStart = now
				d.above = false
				d.aboveStart = time.Time{}
			}
			if now.Sub(d.recoverStart) >= d.cfg.RecoveryHold {
				d.level = d.level - 1
				d.recovering = false
			}
		} else {
			// 重新越界，取消恢复计时
			d.recovering = false
			d.recoverStart = time.Time{}
		}
		return d.level
	}

	// 同一级别：清理状态
	d.above = false
	d.aboveStart = time.Time{}
	d.recovering = false
	d.recoverStart = time.Time{}
	return d.level
}

// thresholdFor 返回某级别对应的触发阈值
func thresholdFor(cfg Config, l Level) float64 {
	switch l {
	case L1:
		return cfg.TriggerL1
	case L2:
		return cfg.TriggerL2
	case L3:
		return cfg.TriggerL3
	}
	return cfg.TriggerL1
}

// Level 当前降级等级
func (d *Degrader) Level() Level {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level
}

// Snapshot 当前状态快照
func (d *Degrader) Snapshot() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return map[string]any{
		"级别":     int(d.level),
		"级别名称":   d.level.Name(),
		"说明":     explain(d.level),
	}
}

func explain(l Level) string {
	switch l {
	case L0:
		return "全部能力可用"
	case L1:
		return "限制非关键后台任务"
	case L2:
		return "暂停后台与编排任务"
	case L3:
		return "仅保留核心 API"
	}
	return ""
}
