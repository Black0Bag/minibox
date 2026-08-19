// Package timestamp 提供全局时间戳服务（B22：NTP 校准 + 全局单调序号）。
//
// 设计依据：
//   - PRD B22：时间戳全局化（YYYY-MM-DD HH:MM:SS + NTP 校准 + 全局单调序号）
//   - dev/02_后端开发路线图.md Phase 0：时间戳组件（系统时钟 + NTP 每小时校准，断网回退 + 序号兜底）
//   - T-02：NTP 库 = beevik/ntp（启动同步 + 每小时校正）
//
// 关键实践（互联网 2026 校准）：
//   - beevik/ntp Response.ClockOffset 是偏移量，应加到 time.Now() 上（不是直接用 Response.Time）
//   - NTP 查询失败时优雅降级到系统时钟（断网回退），不阻塞启动
//   - 全局单调序号用 atomic.Uint64，从 1 开始（跨设备排序靠数据库自增主键兜底）
//   - NTP 查询设超时（默认 5s），避免无网络时拖慢启动
//   - 闰秒（LeapIndicator）只记录不修正（系统级闰秒处理由 OS 层负责）
package timestamp

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beevik/ntp"
)

// Layout 时间戳格式（B22：YYYY-MM-DD HH:MM:SS）。
const Layout = "2006-01-02 15:04:05"

// defaultNTPServers 默认 NTP 服务器池（按优先级尝试）。
// 使用 pool.ntp.org 池（全球负载均衡）+ 国内备用（腾讯/阿里）。
var defaultNTPServers = []string{
	"pool.ntp.org:123",
	"ntp.aliyun.com:123",
	"time1.cloud.tencent.com:123",
}

// Service 全局时间戳服务。
// 启动时同步 NTP，之后每小时校准一次；NTP 不可用时降级到系统时钟。
type Service struct {
	mu           sync.RWMutex
	offset       time.Duration // NTP 时钟偏移（校准后的时间 = time.Now() + offset）
	ntpOK        bool          // NTP 是否成功同步过
	seq          atomic.Uint64 // 全局单调序号（从 1 开始递增）
	logger       *slog.Logger
	servers      []string
	ntpTimeout   time.Duration
	syncInterval time.Duration
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// New 创建时间戳服务。
// logger 为空时用 slog.Default()。
func New(logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:       logger,
		servers:      defaultNTPServers,
		ntpTimeout:   5 * time.Second,
		syncInterval: time.Hour, // 每小时校准一次（T-02）
	}
}

// Start 启动时间戳服务：立即同步一次 NTP，然后定期校准。
// NTP 同步失败不返回错误（优雅降级到系统时钟）。
func (s *Service) Start(ctx context.Context) {
	// 立即同步一次
	s.syncOnce()

	// 启动定期校准 goroutine
	innerCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.syncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-innerCtx.Done():
				return
			case <-ticker.C:
				s.syncOnce()
			}
		}
	}()
}

// Stop 停止定期校准。
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// syncOnce 执行一次 NTP 同步（尝试多个服务器，全失败则降级）。
func (s *Service) syncOnce() {
	for _, server := range s.servers {
		resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{
			Timeout: s.ntpTimeout,
		})
		if err != nil {
			s.logger.Debug("NTP 查询失败，尝试下一个服务器", "server", server, "err", err)
			continue
		}
		// 校验响应（检查合法性）
		if err := resp.Validate(); err != nil {
			s.logger.Debug("NTP 响应校验失败", "server", server, "err", err)
			continue
		}
		// kiss of death（服务器拒绝，换下一个）
		if resp.IsKissOfDeath() {
			s.logger.Debug("NTP kiss-of-death", "server", server, "code", resp.KissCode)
			continue
		}
		s.mu.Lock()
		s.offset = resp.ClockOffset
		s.ntpOK = true
		s.mu.Unlock()
		s.logger.Info("NTP 时间同步成功",
			"server", server,
			"offset_ms", resp.ClockOffset.Milliseconds(),
			"rtt_ms", resp.RTT.Milliseconds(),
			"stratum", resp.Stratum,
		)
		return
	}

	// 所有服务器都失败：降级到系统时钟
	s.mu.Lock()
	wasOK := s.ntpOK
	s.mu.Unlock()
	if !wasOK {
		s.logger.Warn("NTP 同步失败（所有服务器不可达），降级到系统时钟", "servers", s.servers)
	} else {
		s.logger.Warn("NTP 定期校准失败，保持上次偏移量（断网回退）")
	}
}

// Now 返回当前校准时间（NTP 校准后的 time.Time）。
// NTP 不可用时返回系统时钟（优雅降级）。
func (s *Service) Now() time.Time {
	s.mu.RLock()
	offset := s.offset
	s.mu.RUnlock()
	return time.Now().Add(offset)
}

// Format 返回当前时间的格式化字符串（YYYY-MM-DD HH:MM:SS）。
func (s *Service) Format() string {
	return s.Now().Format(Layout)
}

// FormatTime 将指定时间格式化为 YYYY-MM-DD HH:MM:SS。
func FormatTime(t time.Time) string {
	return t.Format(Layout)
}

// NextSeq 返回下一个全局单调序号（从 1 开始，原子递增）。
// 用于跨模块排序保证（数据库自增主键兜底跨设备排序）。
func (s *Service) NextSeq() uint64 {
	return s.seq.Add(1)
}

// NTPSynced 返回 NTP 是否成功同步过。
func (s *Service) NTPSynced() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ntpOK
}

// Offset 返回当前 NTP 时钟偏移量（用于调试/展示）。
func (s *Service) Offset() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.offset
}
