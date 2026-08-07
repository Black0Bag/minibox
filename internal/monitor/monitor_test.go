package monitor

import (
	"testing"
	"time"
)

func TestCollectBasicFields(t *testing.T) {
	c := NewCollector()
	m, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect 错误: %v", err)
	}
	if m.Platform == "" || m.Arch == "" || m.GoVersion == "" {
		t.Error("平台/架构/Go 版本不应为空")
	}
	if m.NumCPU <= 0 {
		t.Errorf("NumCPU 应大于 0，得到 %d", m.NumCPU)
	}
	if m.Process.GoRoutines <= 0 {
		t.Errorf("Go 协程数应大于 0，得到 %d", m.Process.GoRoutines)
	}
	if m.CollectedAt == "" {
		t.Error("采集时间不应为空")
	}
}

func TestCollectStableCPU(t *testing.T) {
	c := NewCollector()
	_, _ = c.Collect() // 首次采样
	time.Sleep(50 * time.Millisecond)
	m, err := c.Collect() // 第二次采样计算差值
	if err != nil {
		t.Fatalf("Collect 错误: %v", err)
	}
	if m.CPU.Percent < 0 || m.CPU.Percent > 100 {
		t.Errorf("CPU 使用率应在 [0,100]，得到 %.2f", m.CPU.Percent)
	}
}

func TestCollectTwiceRates(t *testing.T) {
	c := NewCollector()
	_, _ = c.Collect()
	time.Sleep(20 * time.Millisecond)
	m, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect 错误: %v", err)
	}
	// 网络速率不应为负
	if m.Network.RxRate > (1 << 62) || m.Network.TxRate > (1 << 62) {
		t.Errorf("网络速率异常过大: rx=%d tx=%d", m.Network.RxRate, m.Network.TxRate)
	}
}
