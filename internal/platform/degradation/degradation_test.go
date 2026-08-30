package degradation

import "testing"

// TestLevelString 等级可读名映射（含未知值兜底）。
func TestLevelString(t *testing.T) {
	cases := []struct {
		level Level
		want  string
	}{
		{LevelFull, "全功能"},
		{LevelReduced, "减上下文"},
		{LevelNoEmbed, "禁向量"},
		{LevelTextOnly, "纯文本"},
		{Level(99), "未知"},
		{Level(-1), "未知"},
	}
	for _, tc := range cases {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("Level(%d).String()=%q, 期望 %q", tc.level, got, tc.want)
		}
	}
}

// TestNewMonitorDefaults 新建监控器默认处于 L0 全功能。
func TestNewMonitorDefaults(t *testing.T) {
	m := NewMonitor()
	if got := m.Level(); got != LevelFull {
		t.Errorf("初始等级=%v, 期望 LevelFull", got)
	}
	if !m.EmbedEnabled() {
		t.Error("L0 应启用向量检索")
	}
	if got := m.ContextBudget(); got != 128000 {
		t.Errorf("L0 预算=%d, 期望 128000", got)
	}
}

// TestContextBudgetPerLevel 各等级的上下文预算单调递减。
func TestContextBudgetPerLevel(t *testing.T) {
	cases := []struct {
		level Level
		want  int
	}{
		{LevelFull, 128000},
		{LevelReduced, 32000},
		{LevelNoEmbed, 16000},
		{LevelTextOnly, 8000},
		{Level(99), 8000}, // 未知等级兜底最保守
	}
	for _, tc := range cases {
		m := NewMonitor()
		m.level = tc.level
		if got := m.ContextBudget(); got != tc.want {
			t.Errorf("Level=%v 预算=%d, 期望 %d", tc.level, got, tc.want)
		}
	}
}

// TestEmbedEnabledPerLevel L0/L1 允许向量，L2/L3 禁用。
func TestEmbedEnabledPerLevel(t *testing.T) {
	cases := []struct {
		level Level
		want  bool
	}{
		{LevelFull, true},
		{LevelReduced, true},
		{LevelNoEmbed, false},
		{LevelTextOnly, false},
	}
	for _, tc := range cases {
		m := NewMonitor()
		m.level = tc.level
		if got := m.EmbedEnabled(); got != tc.want {
			t.Errorf("Level=%v EmbedEnabled=%v, 期望 %v", tc.level, got, tc.want)
		}
	}
}

// TestTick_RiseHysteresis 上升迟滞：连续高负载达到 riseCount 才升一级，且一次只升一级。
func TestTick_RiseHysteresis(t *testing.T) {
	m := NewMonitor()
	// 阈值设为 -1：usedMB 最小为 0，必然 > -1，保证每次采样都判为高负载。
	// （不能用 0：测试二进制 Alloc 不足 1MB 时 usedMB==0，0 > 0 不成立会落进迟滞死区）
	m.memHighMB = -1
	m.memLowMB = -2 // 永不触发低负载分支

	// 前 riseCount-1 次不应升级
	for i := 1; i < m.riseCount; i++ {
		lvl, changed := m.Tick()
		if changed {
			t.Fatalf("第 %d 次采样就升级了（应等到第 %d 次）", i, m.riseCount)
		}
		if lvl != LevelFull {
			t.Fatalf("第 %d 次采样等级=%v, 期望仍为 LevelFull", i, lvl)
		}
	}
	// 第 riseCount 次升到 L1
	lvl, changed := m.Tick()
	if !changed {
		t.Fatal("达到 riseCount 应升级")
	}
	if lvl != LevelReduced {
		t.Fatalf("升级后等级=%v, 期望 LevelReduced", lvl)
	}
}

// TestTick_RiseCapsAtTextOnly 上升封顶：不会超过 L3。
func TestTick_RiseCapsAtTextOnly(t *testing.T) {
	m := NewMonitor()
	m.memHighMB = -1
	m.memLowMB = -2
	m.level = LevelTextOnly

	// 持续高负载多轮，等级必须停在 L3
	for range m.riseCount * 3 {
		lvl, changed := m.Tick()
		if changed {
			t.Fatalf("已在 L3 不应再升级，实际变为 %v", lvl)
		}
		if lvl != LevelTextOnly {
			t.Fatalf("等级=%v, 期望封顶在 LevelTextOnly", lvl)
		}
	}
}

// TestTick_FallHysteresis 下降迟滞：连续低负载达到 fallCount 才降一级。
func TestTick_FallHysteresis(t *testing.T) {
	m := NewMonitor()
	// 阈值设为极大：任何内存占用都算低负载
	m.memHighMB = 1 << 30
	m.memLowMB = 1 << 30
	m.level = LevelTextOnly

	for i := 1; i < m.fallCount; i++ {
		lvl, changed := m.Tick()
		if changed {
			t.Fatalf("第 %d 次采样就降级了（应等到第 %d 次）", i, m.fallCount)
		}
		if lvl != LevelTextOnly {
			t.Fatalf("第 %d 次等级=%v, 期望仍为 LevelTextOnly", i, lvl)
		}
	}
	lvl, changed := m.Tick()
	if !changed {
		t.Fatal("达到 fallCount 应降级")
	}
	if lvl != LevelNoEmbed {
		t.Fatalf("降级后等级=%v, 期望 LevelNoEmbed", lvl)
	}
}

// TestTick_FallFloorAtFull 下降下限：不会低于 L0。
func TestTick_FallFloorAtFull(t *testing.T) {
	m := NewMonitor()
	m.memHighMB = 1 << 30
	m.memLowMB = 1 << 30
	m.level = LevelFull

	for range m.fallCount * 3 {
		lvl, changed := m.Tick()
		if changed {
			t.Fatalf("已在 L0 不应再降级，实际变为 %v", lvl)
		}
		if lvl != LevelFull {
			t.Fatalf("等级=%v, 期望保持 LevelFull", lvl)
		}
	}
}

// TestTick_CounterResetOnFlip 计数器互斥：高负载清零低计数，反之亦然，防止抖动累积误升降。
func TestTick_CounterResetOnFlip(t *testing.T) {
	m := NewMonitor()

	// 先攒 riseCount-1 次高负载
	m.memHighMB = -1
	m.memLowMB = -2
	for range m.riseCount - 1 {
		m.Tick()
	}
	if m.highCount != m.riseCount-1 {
		t.Fatalf("highCount=%d, 期望 %d", m.highCount, m.riseCount-1)
	}

	// 切到低负载：highCount 必须被清零
	m.memHighMB = 1 << 30
	m.memLowMB = 1 << 30
	m.Tick()
	if m.highCount != 0 {
		t.Errorf("切换到低负载后 highCount=%d, 期望 0", m.highCount)
	}
	if m.lowCount == 0 {
		t.Error("低负载应累积 lowCount")
	}

	// 再切回高负载：lowCount 必须被清零
	m.memHighMB = -1
	m.memLowMB = -2
	m.Tick()
	if m.lowCount != 0 {
		t.Errorf("切换到高负载后 lowCount=%d, 期望 0", m.lowCount)
	}
}

// TestTick_DeadZoneNoChange 迟滞带内（low <= used <= high）不改变等级也不累积计数。
func TestTick_DeadZoneNoChange(t *testing.T) {
	m := NewMonitor()
	// 让当前占用必然落在 (memLowMB, memHighMB] 区间内：
	// usedMB >= 0 不小于 0，也不大于 1<<30，两个分支都不触发
	m.memLowMB = 0
	m.memHighMB = 1 << 30
	m.level = LevelReduced

	for range 10 {
		lvl, changed := m.Tick()
		if changed {
			t.Fatalf("迟滞带内不应变更等级，实际变为 %v", lvl)
		}
		if lvl != LevelReduced {
			t.Fatalf("等级=%v, 期望保持 LevelReduced", lvl)
		}
	}
	if m.highCount != 0 || m.lowCount != 0 {
		t.Errorf("迟滞带内不应累积计数：high=%d low=%d", m.highCount, m.lowCount)
	}
}
