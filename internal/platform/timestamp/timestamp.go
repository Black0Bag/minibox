// Package timestamp 提供时间戳全局化（B22：UTC + 全局单调序号）。
// 设计：PRD B22，每次调用 Tick() 自增序号，保证全局单调（重启不保证）。
// 时间格式：YYYY-MM-DD HH:MM:SS（UTC），单调序号与 NTP 校准（Phase 3 后续）。
package timestamp

import (
	"sync/atomic"
	"time"
)

var seq atomic.Int64

// Tick 返回一条全局单调时间戳。
// 格式："2026-08-16 17:30:00.000Z seq=00042"
func Tick() string {
	n := seq.Add(1)
	return time.Now().UTC().Format("2006-01-02 15:04:05.000Z") + " seq=" + formatSeq(n)
}

// TickNow 返回当前时间与序号（不递增）。
func TickNow() string {
	n := seq.Load()
	return time.Now().UTC().Format("2006-01-02 15:04:05.000Z") + " seq=" + formatSeq(n)
}

func formatSeq(n int64) string {
	s := ""
	v := n
	for v > 0 || len(s) < 5 {
		s = string(rune('0'+v%10)) + s
		v /= 10
	}
	return s
}
