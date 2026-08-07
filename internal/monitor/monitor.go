package monitor

import (
	"runtime"
	"sync"
	"time"
)

// Metrics 系统性能指标快照（性能监控 API 数据源）
type Metrics struct {
	CPU         CPUStats     `json:"cpu"`
	Memory      MemoryStats  `json:"memory"`
	Disk        DiskStats    `json:"disk"`
	Network     NetworkStats `json:"network"`
	Process     ProcessStats `json:"process"`
	Platform    string       `json:"platform"`
	Arch        string       `json:"arch"`
	GoVersion   string       `json:"go_version"`
	NumCPU      int          `json:"num_cpu"`
	CollectedAt string       `json:"collected_at"`
}

// CPUStats CPU 使用率（%）
type CPUStats struct {
	Percent float64 `json:"percent"`
	Count   int     `json:"count"`
}

// MemoryStats 内存使用
type MemoryStats struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// DiskStats 磁盘使用
type DiskStats struct {
	TotalBytes  uint64  `json:"total_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// NetworkStats 网络累计流量（差值采样）
type NetworkStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
	RxRate  uint64 `json:"rx_rate_bps"`
	TxRate  uint64 `json:"tx_rate_bps"`
}

// ProcessStats 进程信息
type ProcessStats struct {
	ProcessCount int   `json:"process_count"`
	GoRoutines   int   `json:"go_routines"`
	GoHeapMB     int64 `json:"go_heap_mb"`
}

// Collector 系统指标采集器（跨平台：Linux 完整采集，其他平台基础指标）
type Collector struct {
	mu       sync.Mutex
	lastCPUT cpuTotal
	lastNet  netTotal
	lastTime time.Time
}

// NewCollector 创建采集器
func NewCollector() *Collector {
	return &Collector{}
}

// Collect 采集当前指标快照
func (c *Collector) Collect() (*Metrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	m := &Metrics{
		Platform:    runtime.GOOS,
		Arch:        runtime.GOARCH,
		GoVersion:   runtime.Version(),
		NumCPU:      runtime.NumCPU(),
		CollectedAt: now.Format("2006-01-02 15:04:05"),
	}
	c.collectPlatform(m, now)

	// 进程 / Go 运行时信息（跨平台）
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.Process.GoRoutines = runtime.NumGoroutine()
	m.Process.GoHeapMB = int64(ms.Alloc / 1024 / 1024)
	m.Process.ProcessCount = processCount()

	// CPU 使用率：基于两次采样差值（比例）
	if c.lastCPUT.valid && !c.lastTime.IsZero() {
		dt := now.Sub(c.lastTime).Seconds()
		if dt > 0 {
			busy := float64(c.currentCPUT().busy - c.lastCPUT.busy)
			total := float64(c.currentCPUT().total - c.lastCPUT.total)
			if total > 0 {
				pct := (busy / total) * 100
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}
				m.CPU.Percent = pct
			}
		}
	}
	m.CPU.Count = runtime.NumCPU()
	return m, nil
}
