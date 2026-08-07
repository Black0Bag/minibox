//go:build !linux

package monitor

import "time"

// 非 Linux 平台：提供基础指标，CPU/磁盘/网络等标为不可用（0 / 空）
type cpuTotal struct {
	busy  uint64
	total uint64
	valid bool
}

type netTotal struct {
	rx, tx uint64
	valid  bool
}

func (c *Collector) currentCPUT() cpuTotal { return cpuTotal{} }

// collectPlatform 非 Linux 平台：仅采集内存（进程级）
func (c *Collector) collectPlatform(m *Metrics, _ time.Time) {
	// 无系统级指标，置零
	m.Memory = MemoryStats{}
	m.Disk = DiskStats{}
	m.Network = NetworkStats{}
}

func processCount() int { return 0 }
