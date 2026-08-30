// Package monitor 提供系统性能监控指标采集（Phase 3.5 性能监控 API）。
// 纯 Go 实现，零 CGO 依赖：
//   - 系统 CPU：两次采样 /proc/stat 的 jiffies 差值（Linux 主部署目标）
//   - 系统内存：/proc/meminfo（优先 MemAvailable）
//   - 磁盘：syscall.Statfs（当前工作目录所在分区）
//   - 进程指标：runtime.MemStats + goroutine 数
//
// 非 Linux 平台自动降级：CPU/内存返回 0，不报错（通用降级原则）。
package monitor

import (
	"bufio"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SystemStats 系统级指标。
type SystemStats struct {
	// CPUPercent 系统 CPU 使用率（0.0~100.0，两次采样差值）。
	CPUPercent float64 `json:"cpu_percent"`
	// MemoryTotalBytes 物理内存总量。
	MemoryTotalBytes uint64 `json:"memory_total_bytes"`
	// MemoryUsedBytes 已用内存（总量 - MemAvailable）。
	MemoryUsedBytes uint64 `json:"memory_used_bytes"`
	// MemoryPercent 内存使用率（0.0~100.0）。
	MemoryPercent float64 `json:"memory_percent"`
	// DiskUsedBytes 当前目录所在分区已用空间。
	DiskUsedBytes uint64 `json:"disk_used_bytes"`
	// DiskTotalBytes 当前目录所在分区总空间。
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
	// DiskPercent 磁盘使用率（0.0~100.0）。
	DiskPercent float64 `json:"disk_percent"`
}

// ProcessStats 进程级指标（Go runtime）。
type ProcessStats struct {
	// Goroutines 当前 goroutine 数。
	Goroutines int `json:"goroutines"`
	// HeapAllocBytes 堆内存已分配字节数。
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	// SysBytes 从 OS 申请的总字节数。
	SysBytes uint64 `json:"sys_bytes"`
	// NumGC 累计 GC 次数。
	NumGC uint32 `json:"num_gc"`
	// GoVersion Go 编译器版本。
	GoVersion string `json:"go_version"`
	// OS 运行平台操作系统。
	OS string `json:"os"`
	// Arch 运行平台架构。
	Arch string `json:"arch"`
}

// Metrics 完整指标快照（系统级 + 进程级）。
type Metrics struct {
	System  SystemStats  `json:"system"`
	Process ProcessStats `json:"process"`
	// Timestamp 采集时间戳。
	Timestamp time.Time `json:"timestamp"`
}

// cpuTimes /proc/stat 的 cpu 聚合行时间片。
type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

// total 总时间片。
func (c cpuTimes) total() uint64 {
	return c.user + c.nice + c.system + c.idle + c.iowait + c.irq + c.softirq + c.steal
}

// busy 非空闲时间片（idle/iowait 之外）。
func (c cpuTimes) busy() uint64 {
	return c.user + c.nice + c.system + c.irq + c.softirq + c.steal
}

// Collector 系统性能采集器（线程安全）。
type Collector struct {
	mu sync.Mutex
	// lastCPU 上一次采样的 cpu 聚合时间片（差值计算用）。
	lastCPU cpuTimes
	// hasLast 是否已有历史采样（首次采样无法计算差值）。
	hasLast bool
}

// NewCollector 创建性能采集器。
// 立即做一次基线采样，使第二次 Collect 即可输出 CPU 使用率。
func NewCollector() *Collector {
	c := &Collector{}
	if t, ok := readProcStat(); ok {
		c.lastCPU = t
		c.hasLast = true
	}
	return c
}

// Collect 采集一次完整指标快照。
func (c *Collector) Collect() Metrics {
	c.mu.Lock()
	cpuPercent := c.cpuPercentLocked()
	c.mu.Unlock()

	memTotal, memAvail := readProcMeminfo()
	memUsed := uint64(0)
	if memTotal > memAvail {
		memUsed = memTotal - memAvail
	}
	memPercent := 0.0
	if memTotal > 0 {
		memPercent = float64(memUsed) / float64(memTotal) * 100.0
	}

	diskUsed, diskTotal := diskUsage()
	diskPercent := 0.0
	if diskTotal > 0 {
		diskPercent = float64(diskUsed) / float64(diskTotal) * 100.0
	}

	return Metrics{
		System: SystemStats{
			CPUPercent:       cpuPercent,
			MemoryTotalBytes: memTotal,
			MemoryUsedBytes:  memUsed,
			MemoryPercent:    memPercent,
			DiskUsedBytes:    diskUsed,
			DiskTotalBytes:   diskTotal,
			DiskPercent:      diskPercent,
		},
		Process:   processStats(),
		Timestamp: time.Now(),
	}
}

// cpuPercentLocked 计算 CPU 使用率（调用方需持锁）。
// 方法：两次采样 busy/total 差值 → (busyΔ/totalΔ)×100（jiffies 差值，防调度漂移）。
func (c *Collector) cpuPercentLocked() float64 {
	cur, ok := readProcStat()
	if !ok {
		return 0 // 非 Linux / 读取失败 → 降级 0
	}
	if !c.hasLast {
		c.lastCPU = cur
		c.hasLast = true
		return 0 // 首次采样无差值
	}
	totalDelta := cur.total() - c.lastCPU.total()
	busyDelta := cur.busy() - c.lastCPU.busy()
	c.lastCPU = cur
	if totalDelta == 0 {
		return 0
	}
	p := float64(busyDelta) / float64(totalDelta) * 100.0
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

// readProcStat 读取 /proc/stat 的 cpu 聚合行。
// 返回 (时间片, 是否成功)。非 Linux 平台返回 false（自动降级）。
func readProcStat() (cpuTimes, bool) {
	f, err := os.Open("/proc/stat") // #nosec G304 -- 固定系统路径，非用户输入
	if err != nil {
		return cpuTimes{}, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// fields[0]="cpu"，后续为 user nice system idle iowait irq softirq steal ...
		var vals []uint64
		for _, s := range fields[1:] {
			v, err := parseUint(s)
			if err != nil {
				return cpuTimes{}, false
			}
			vals = append(vals, v)
		}
		var t cpuTimes
		if len(vals) > 0 {
			t.user = vals[0]
		}
		if len(vals) > 1 {
			t.nice = vals[1]
		}
		if len(vals) > 2 {
			t.system = vals[2]
		}
		if len(vals) > 3 {
			t.idle = vals[3]
		}
		if len(vals) > 4 {
			t.iowait = vals[4]
		}
		if len(vals) > 5 {
			t.irq = vals[5]
		}
		if len(vals) > 6 {
			t.softirq = vals[6]
		}
		if len(vals) > 7 {
			t.steal = vals[7]
		}
		return t, true
	}
	return cpuTimes{}, false
}

// readProcMeminfo 读取 /proc/meminfo 的 MemTotal/MemAvailable。
// 返回 (总量, 可用)，单位字节。非 Linux 平台返回 (0, 0)（自动降级）。
func readProcMeminfo() (total uint64, available uint64) {
	f, err := os.Open("/proc/meminfo") // #nosec G304 -- 固定系统路径，非用户输入
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()

	// MemAvailable 缺失时回退 MemFree + Cached（内核 3.14 之前）
	var free, cached uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, _ = parseUint(fields[1])
		case "MemAvailable:":
			available, _ = parseUint(fields[1])
		case "MemFree:":
			free, _ = parseUint(fields[1])
		case "Cached:":
			cached, _ = parseUint(fields[1])
		}
	}
	// MemAvailable 缺失 → MemFree+Cached 兜底
	if available == 0 && total > 0 {
		available = free + cached
	}
	return total * 1024, available * 1024 // /proc/meminfo 单位是 kB → 字节
}

// parseUint 解析十进制无符号数（轻量实现，避免 fmt 开销）。
func parseUint(s string) (uint64, error) {
	if s == "" {
		return 0, os.ErrInvalid
	}
	var out uint64
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, os.ErrInvalid
		}
		out = out*10 + uint64(s[i]-'0')
	}
	return out, nil
}

// diskUsage 获取当前目录所在分区的磁盘使用情况（syscall.Statfs，Unix 标准接口）。
func diskUsage() (used uint64, total uint64) {
	wd, err := os.Getwd()
	if err != nil {
		return 0, 0
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(wd, &stat); err != nil {
		return 0, 0
	}
	// 非特权用户可见空间 = Blocks - Bavail（Bfree 含 root 保留块）
	total = stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize) // Bavail = 普通用户可用块
	if total < free {
		return 0, total
	}
	return total - free, total
}

// processStats 采集 Go 进程自身指标（runtime，跨平台）。
func processStats() ProcessStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return ProcessStats{
		Goroutines:     runtime.NumGoroutine(),
		HeapAllocBytes: m.HeapAlloc,
		SysBytes:       m.Sys,
		NumGC:          m.NumGC,
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}
}
