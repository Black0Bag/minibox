package timestamp

import (
	"log/slog"
	"sync"
	"time"

	"github.com/beevik/ntp"
)

// 统一时间戳格式（滚动追加记录 / logme / 审计等一切时间戳场景）
const TimeFormat = "2006-01-02 15:04:05"

// Service 统一时间服务：系统时钟 + NTP 校准（T-02）
// 时间戳负责真实性/展示（系统时钟+NTP），单调序号负责有序性（数据库自增主键）。
type Service struct {
	mu      sync.RWMutex
	offset  time.Duration
	ntpHost string
}

// New 创建时间服务。ntpHost 为空则用默认 NTP 服务器。
func New(ntpHost string) *Service {
	if ntpHost == "" {
		ntpHost = "pool.ntp.org"
	}
	return &Service{ntpHost: ntpHost}
}

// Sync 立即做一次 NTP 校准。失败时不阻塞（回退系统时钟，序号兜底保证有序）。
func (s *Service) Sync() error {
	resp, err := ntp.Query(s.ntpHost)
	if err != nil {
		slog.Warn("NTP 同步失败，回退系统时钟", "host", s.ntpHost, "err", err)
		return err
	}
	s.mu.Lock()
	s.offset = resp.ClockOffset
	s.mu.Unlock()
	slog.Info("NTP 时间同步成功", "host", s.ntpHost, "offset", resp.ClockOffset.String())
	return nil
}

// Now 返回校准后的当前时间
func (s *Service) Now() time.Time {
	s.mu.RLock()
	off := s.offset
	s.mu.RUnlock()
	return time.Now().Add(off)
}

// Format 格式化为统一时间戳 YYYY-MM-DD HH:MM:SS
func (s *Service) Format(t time.Time) string {
	return t.Format(TimeFormat)
}

// FormatNow 返回当前时间的统一时间戳字符串
func (s *Service) FormatNow() string {
	return s.Now().Format(TimeFormat)
}

// StartPeriodicSync 启动周期性 NTP 校准（默认每小时），返回停止函数。
func (s *Service) StartPeriodicSync(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = s.Sync()
			case <-stop:
				return
			}
		}
	}()
}
