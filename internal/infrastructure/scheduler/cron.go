// Package scheduler 提供调度中枢的 in-process 实现（robfig/cron v3）。
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Black0Bag/minibox/internal/domain/scheduler"
)

// CronScheduler 基于 robfig/cron 的 in-process 调度器实现。
// 单二进制 in-process（cronicle/libtnb-cron 实证），不引分布式。
type CronScheduler struct {
	mu     sync.RWMutex
	cron   *cron.Cron
	tasks  map[string]scheduler.Task
	entry  map[string]cron.EntryID // taskID → cron entryID（Remove 用）
	alarms map[string]*time.Timer  // taskID → 单次闹钟定时器（alarm 用）
	runner scheduler.Runner
	logger *slog.Logger
	// id 计数器
	nextID int
}

// New 创建调度器。
func New(runner scheduler.Runner, logger *slog.Logger) *CronScheduler {
	// SkipIfStillRunning：任务执行超时重叠时跳过后续触发，防止并发叠加实例（预算失效）
	c := cron.New(
		cron.WithSeconds(),
		cron.WithChain(cron.SkipIfStillRunning(&cronLogger{logger: logger})),
	)
	return &CronScheduler{
		cron:   c,
		tasks:  make(map[string]scheduler.Task),
		entry:  make(map[string]cron.EntryID),
		alarms: make(map[string]*time.Timer),
		runner: runner,
		logger: logger,
	}
}

// cronLogger 把 *slog.Logger 适配为 robfig/cron 的 Logger 接口。
type cronLogger struct {
	logger *slog.Logger
}

// Info 实现 cron.Logger。
func (l *cronLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, keysAndValues...)
}

// Error 实现 cron.Logger。
func (l *cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.logger.Error(msg, append(keysAndValues, "err", err)...)
}

var _ cron.Logger = (*cronLogger)(nil)

// Add 注册任务。
// - TypeAlarm（At 非空）：time.AfterFunc 单次调度，触发后自动从表移除
// - 其他类型：cron 表达式周期调度
// - Enabled=false 拒绝注册（disabled 任务不应执行）
func (s *CronScheduler) Add(task scheduler.Task) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !task.Enabled {
		return "", fmt.Errorf("任务未启用（Enabled=false）")
	}

	// 分配任务 ID
	s.nextID++
	id := fmt.Sprintf("task_%d", s.nextID)
	task.ID = id

	// alarm 单次闹钟：At 定时，触发后移除
	if task.Type == scheduler.TypeAlarm && task.At != nil {
		delay := time.Until(*task.At)
		if delay < 0 {
			return "", fmt.Errorf("闹钟时间已过期: %s", task.At.Format(time.RFC3339))
		}
		timer := time.AfterFunc(delay, func() {
			s.runTask(task)
			s.removeAlarm(id)
		})
		s.alarms[id] = timer
		s.tasks[id] = task
		s.logger.Info("闹钟已注册", "id", id, "name", task.Name, "at", task.At.Format(time.RFC3339))
		return id, nil
	}

	// 周期/日历任务：cron 表达式
	entryID, err := s.cron.AddFunc(task.Spec, func() {
		s.runTask(task)
	})
	if err != nil {
		return "", fmt.Errorf("cron 表达式无效: %w", err)
	}
	s.entry[id] = entryID

	s.tasks[id] = task
	s.logger.Info("调度任务已注册", "id", id, "name", task.Name, "spec", task.Spec)
	return id, nil
}

// removeAlarm 移除已触发的闹钟（触发后自清理）。
func (s *CronScheduler) removeAlarm(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	delete(s.alarms, id)
}

// Remove 移除任务。
func (s *CronScheduler) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	if eid, ok := s.entry[id]; ok {
		s.cron.Remove(eid)
		delete(s.entry, id)
	}
	if timer, ok := s.alarms[id]; ok {
		timer.Stop()
		delete(s.alarms, id)
	}
	delete(s.tasks, id)
	return nil
}

// List 列出所有任务。
func (s *CronScheduler) List() []scheduler.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]scheduler.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

// Start 启动调度（阻塞直到 ctx 取消）。
func (s *CronScheduler) Start(ctx context.Context) error {
	s.cron.Start()
	defer s.cron.Stop()
	<-ctx.Done()
	return nil
}

// Stop 停止调度（cron 停止 + 未触发的闹钟取消）。
func (s *CronScheduler) Stop() {
	s.cron.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, timer := range s.alarms {
		timer.Stop()
		delete(s.alarms, id)
	}
}

// runTask 执行任务（带预算 + panic 隔离）。
// 注意：robfig/cron 每个 job 触发时已在自己的 goroutine 中运行，
// 本函数必须同步阻塞，否则 SkipIfStillRunning chain 无法感知运行状态、
// 重叠保护失效（预算会被叠加实例突破）。
func (s *CronScheduler) runTask(task scheduler.Task) {
	// panic 隔离：任务执行异常不拖垮调度器（golang-safety）
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("调度任务 panic", "id", task.ID, "recover", r)
		}
	}()

	if s.runner == nil {
		s.logger.Warn("调度任务无执行器（runner 未注入）", "id", task.ID)
		return
	}

	// 预算墙钟 ctx 在函数内创建并 defer cancel，
	// 保证 ctx 存活到任务结束（否则返回即取消，任务启动就失败）。
	ctx := context.Background()
	if task.Budget.MaxWall > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, task.Budget.MaxWall)
		defer cancel()
	}

	result, tokens, err := s.runner.Run(ctx, task)
	if err != nil {
		s.logger.Warn("调度任务执行失败", "id", task.ID, "err", err)
		return
	}
	s.logger.Info("调度任务完成", "id", task.ID, "tokens", tokens, "result_len", len(result))
}
