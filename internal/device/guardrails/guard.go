// Package guardrails 设备操作护栏（D-17）。
// 设计：系统设计/03_设备代理方案.md
// 三要素：危险动作清单 / 频率限制 / HITL 确认钩子。
package guardrails

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Action 设备动作。
type Action struct {
	DeviceID string
	Method   string
	Params   map[string]any
}

// Decision 护栏决策三态。
type Decision string

const (
	// Allow 允许执行。
	Allow Decision = "allow"
	// Ask 需要人工确认。
	Ask Decision = "ask"
	// Deny 拒绝执行。
	Deny Decision = "deny"
)

// DangerRule 危险动作规则。
type DangerRule struct {
	Method     string // 匹配的方法名（如 设备_输入文字）
	RequireAsk bool   // 是否必须 HITL 确认
	RateLimit  int    // 每分钟允许次数（0=不限）
}

// DefaultDangerRules 默认危险动作清单（D-13/D-17）。
// 危险度分级：敏感操作（输入/通知）→ ask；安全读操作（截屏/层级）→ allow。
var DefaultDangerRules = []DangerRule{
	// 眼：安全读操作 → 放行
	{Method: "设备_截屏", RequireAsk: false, RateLimit: 30},
	{Method: "设备_界面层级", RequireAsk: false, RateLimit: 30},
	{Method: "设备_通知列表", RequireAsk: false, RateLimit: 30},
	{Method: "设备_信息", RequireAsk: false, RateLimit: 30},
	{Method: "设备_前台应用", RequireAsk: false, RateLimit: 30},
	{Method: "设备_剪贴板", RequireAsk: true, RateLimit: 10},
	// 手：敏感操作 → ask；普通操作 → 放行
	{Method: "设备_输入文字", RequireAsk: true, RateLimit: 10},
	{Method: "设备_打开应用", RequireAsk: true, RateLimit: 20},
	{Method: "设备_按键", RequireAsk: true, RateLimit: 30},
	{Method: "设备_长按", RequireAsk: true, RateLimit: 30},
	{Method: "设备_滚动查找", RequireAsk: false, RateLimit: 60},
	{Method: "设备_提示", RequireAsk: false, RateLimit: 20},
	{Method: "设备_点击", RequireAsk: false, RateLimit: 60},
	{Method: "设备_滑动", RequireAsk: false, RateLimit: 60},
	// 口：朗读放行，提示放行
	{Method: "设备_朗读", RequireAsk: false, RateLimit: 10},
	// 耳：通知/监听/听写 → ask
	{Method: "设备_监听", RequireAsk: true, RateLimit: 5},
	{Method: "设备_听写", RequireAsk: true, RateLimit: 5},
	{Method: "设备_通知", RequireAsk: true, RateLimit: 10},
}

// HITLCallback 危险操作确认回调（返回 true=放行）。
type HITLCallback func(ctx context.Context, action Action) (bool, error)

// Guard 设备护栏。
type Guard struct {
	mu     sync.Mutex
	rules  map[string]DangerRule
	hitl   HITLCallback
	window time.Duration
	calls  map[string][]time.Time // key: deviceID+"|"+method → 调用时间戳
}

// New 创建设备护栏。
func New() *Guard {
	rules := make(map[string]DangerRule)
	for _, r := range DefaultDangerRules {
		rules[r.Method] = r
	}
	return &Guard{
		rules:  rules,
		window: time.Minute,
		calls:  make(map[string][]time.Time),
	}
}

// SetHITL 设置危险操作确认回调（由 app 层注入）。
func (g *Guard) SetHITL(cb HITLCallback) { g.hitl = cb }

// SetRules 覆盖默认规则。
func (g *Guard) SetRules(rules []DangerRule) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rules = make(map[string]DangerRule)
	for _, r := range rules {
		g.rules[r.Method] = r
	}
}

// Check 检查动作是否允许。
// 返回 Allow（放行）/ Ask（需 HITL 确认）/ Deny（拒绝）。
func (g *Guard) Check(ctx context.Context, action Action) (Decision, string, error) {
	g.mu.Lock()
	rule, ok := g.rules[action.Method]
	g.mu.Unlock()

	if !ok {
		// 未知方法：保守拒绝
		return Deny, fmt.Sprintf("未注册的方法: %s", action.Method), nil
	}

	// 频率限制
	if rule.RateLimit > 0 {
		allowed, err := g.rateLimit(action, rule.RateLimit)
		if err != nil {
			return Deny, err.Error(), nil
		}
		if !allowed {
			return Deny, fmt.Sprintf("方法 %s 超出频率限制", action.Method), nil
		}
	}

	// HITL 确认
	if rule.RequireAsk {
		if g.hitl == nil {
			return Ask, fmt.Sprintf("方法 %s 需要人工确认，但未配置确认回调", action.Method), nil
		}
		ok, err := g.hitl(ctx, action)
		if err != nil {
			return Deny, fmt.Sprintf("HITL 确认失败: %v", err), nil
		}
		if !ok {
			return Deny, "用户拒绝该操作", nil
		}
	}

	return Allow, "", nil
}

// rateLimit 频率限制（滑动窗口）。
func (g *Guard) rateLimit(action Action, limit int) (bool, error) {
	key := action.DeviceID + "|" + action.Method
	now := time.Now()

	g.mu.Lock()
	defer g.mu.Unlock()

	// 清理过期时间戳
	cutoff := now.Add(-g.window)
	times := g.calls[key]
	keep := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	g.calls[key] = keep

	if len(keep) >= limit {
		return false, nil
	}
	g.calls[key] = append(g.calls[key], now)
	return true, nil
}
