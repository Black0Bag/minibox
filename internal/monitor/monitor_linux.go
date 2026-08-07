//go:build linux

package monitor

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// cpuTotal CPU 累计时间
type cpuTotal struct {
	busy  uint64 // 忙时（user+system+iowait+irq+...）
	total uint64 // 总时间
	valid bool
}

// netTotal 网络累计流量
type netTotal struct {
	rx, tx uint64
	valid  bool
}

// readCPU 读取 /proc/stat 第一行 CPU 累计时间
func readCPU() cpuTotal {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTotal{}
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTotal{}
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTotal{}
	}
	var vals []uint64
	for _, s := range fields[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			vals = append(vals, 0)
		} else {
			vals = append(vals, v)
		}
	}
	var idle uint64
	// 前 4 个：user nice system idle
	if len(vals) >= 4 {
		idle = vals[3]
	}
	var total uint64
	for _, v := range vals {
		total += v
	}
	return cpuTotal{busy: total - idle, total: total, valid: total > 0}
}

// currentCPUT 返回当前 CPU 累计（缓存于本次 Collect）
func (c *Collector) currentCPUT() cpuTotal {
	return readCPU()
}

// readNet 读取 /proc/net/dev 全部接口累计流量
func readNet() netTotal {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return netTotal{}
	}
	defer func() { _ = f.Close() }()
	var n netTotal
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		n.rx += rx
		n.tx += tx
	}
	n.valid = true
	return n
}

// readDisk 读取根分区磁盘使用
func readDisk() DiskStats {
	var st unix.Statfs_t
	if err := unix.Statfs("/", &st); err != nil {
		return DiskStats{}
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used := total - free
	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return DiskStats{TotalBytes: total, FreeBytes: free, UsedBytes: used, UsedPercent: pct}
}

// readMem 读取 /proc/meminfo 内存使用
func readMem() MemoryStats {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemoryStats{}
	}
	kv := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		kv[key] = v * 1024 // kB → B
	}
	total := kv["MemTotal"]
	avail := kv["MemAvailable"]
	if avail == 0 {
		avail = kv["MemFree"]
	}
	var used uint64
	var pct float64
	if total > 0 {
		used = total - avail
		pct = float64(used) / float64(total) * 100
	}
	return MemoryStats{
		TotalBytes:     total,
		AvailableBytes: avail,
		UsedBytes:      used,
		UsedPercent:    float64(pct),
	}
}

// processCount 扫描 /proc 进程数
func processCount() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && isNumeric(e.Name()) {
			n++
		}
	}
	return n
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// collectPlatform Linux 平台完整采集
func (c *Collector) collectPlatform(m *Metrics, now time.Time) {
	// CPU 快照
	cur := c.currentCPUT()
	c.lastCPUT = cur

	// 内存
	m.Memory = readMem()
	// 磁盘
	m.Disk = readDisk()
	// 网络
	curNet := readNet()
	if c.lastNet.valid && !c.lastTime.IsZero() {
		dt := now.Sub(c.lastTime).Seconds()
		if dt > 0 {
			m.Network.RxRate = rateDiff(curNet.rx, c.lastNet.rx, dt)
			m.Network.TxRate = rateDiff(curNet.tx, c.lastNet.tx, dt)
		}
	}
	m.Network.RxBytes = curNet.rx
	m.Network.TxBytes = curNet.tx
	c.lastNet = curNet
	c.lastTime = now
}

// rateDiff 计算速率差值（字节/秒）
func rateDiff(cur, prev uint64, dt float64) uint64 {
	if cur <= prev || dt <= 0 {
		return 0
	}
	return uint64(float64(cur-prev) / dt)
}
