package degrade

import (
	"testing"
	"time"
)

// fakeProbe 可编程的探测函数
func fakeProbe(cpu, mem float64) ProbeFunc {
	return func() (float64, float64) { return cpu, mem }
}

func TestInitialLevel(t *testing.T) {
	d := New(fakeProbe(10, 20), Config{})
	if d.Level() != L0 {
		t.Errorf("初始应为 L0，得到 %d", d.Level())
	}
}

func TestNormalStaysL0(t *testing.T) {
	d := New(fakeProbe(30, 40), Config{})
	for i := 0; i < 10; i++ {
		d.Evaluate(time.Now())
	}
	if d.Level() != L0 {
		t.Errorf("低负载应保持 L0，得到 %d", d.Level())
	}
}

func TestTriggerL1(t *testing.T) {
	cfg := Config{TriggerL1: 80, TriggerL2: 90, TriggerL3: 95, TriggerHold: 30 * time.Second, RecoveryHold: 60 * time.Second, Hysteresis: 5}
	d := New(fakeProbe(85, 40), cfg)
	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Evaluate(now)
	}
	if d.Level() != L0 {
		t.Fatalf("未到 30s 不应升级（得到 %d", d.Level())
	}
	// 超过 30s 触发
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L1 {
		t.Errorf("CPU 85%% 持续应升级 L1（得到 %d）", d.Level())
	}
}

func TestTriggerL2AndL3(t *testing.T) {
	cfg := Config{TriggerL1: 80, TriggerL2: 90, TriggerL3: 95, TriggerHold: 30 * time.Second, RecoveryHold: 60 * time.Second, Hysteresis: 5}
	// CPU 97% → 直接 L3（最高级）
	d := New(fakeProbe(97, 40), cfg)
	now := time.Now()
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L3 {
		t.Errorf("CPU 97pct 应 L3（得到 %d）", d.Level())
	}

	// CPU 92% → L2
	d2 := New(fakeProbe(92, 40), cfg)
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Second)
		d2.Evaluate(now)
	}
	if d2.Level() != L2 {
		t.Errorf("CPU 92pct 应 L2（得到 %d", d2.Level())
	}
}

func TestMemoryTriggers(t *testing.T) {
	cfg := Config{TriggerL1: 80, TriggerL2: 90, TriggerL3: 95, TriggerHold: 30 * time.Second, RecoveryHold: 60 * time.Second, Hysteresis: 5}
	// 内存 93%，CPU 低 → 应触发 L2
	d := New(fakeProbe(20, 93), cfg)
	now := time.Now()
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L2 {
		t.Errorf("内存 93pct 应 L2（得到 %d）", d.Level())
	}
}

func TestRecoveryWithHysteresis(t *testing.T) {
	cfg := Config{TriggerL1: 80, TriggerL2: 90, TriggerL3: 95, TriggerHold: 30 * time.Second, RecoveryHold: 60 * time.Second, Hysteresis: 5}
	d := New(fakeProbe(85, 40), cfg)
	now := time.Now()
	// 升到 L1
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L1 {
		t.Fatalf("准备阶段失败: %d", d.Level())
	}
	// 恢复到 76%（低于阈值 80-5=75? 不，76 没低于 75，应保持）→ 先测恢复到 60%
	d.probe = fakeProbe(60, 40)
	// 未到 60s 不应恢复
	for i := 0; i < 2; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L1 {
		t.Fatalf("恢复期未满不应降级，得到 %d", d.Level())
	}
	// 满 60s 恢复
	for i := 0; i < 2; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L0 {
		t.Errorf("恢复应降回 L0，得到 %d", d.Level())
	}
}

func TestHysteresisBand(t *testing.T) {
	cfg := Config{TriggerL1: 80, TriggerL2: 90, TriggerL3: 95, TriggerHold: 30 * time.Second, RecoveryHold: 60 * time.Second, Hysteresis: 5}
	d := New(fakeProbe(82, 40), cfg)
	now := time.Now()
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L1 {
		t.Fatalf("应升 L1: %d", d.Level())
	}
	// 78% 在迟滞带内（80-5=75 ~ 80）：不触发恢复
	d.probe = fakeProbe(78, 40)
	for i := 0; i < 5; i++ {
		now = now.Add(30 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L1 {
		t.Errorf("迟滞带内不应恢复，得到 %d", d.Level())
	}
	// 降到 70%（低于 75）→ 恢复
	d.probe = fakeProbe(70, 40)
	for i := 0; i < 4; i++ {
		now = now.Add(30 * time.Second)
		d.Evaluate(now)
	}
	if d.Level() != L0 {
		t.Errorf("低于迟滞带应恢复 L0，得到 %d", d.Level())
	}
}

func TestLevelMetadata(t *testing.T) {
	if L0.Name() != "正常" || L1.Name() != "轻度" || L2.Name() != "中度" || L3.Name() != "严重" {
		t.Errorf("级别名称错误: %v %v %v %v", L0.Name(), L1.Name(), L2.Name(), L3.Name())
	}
}
