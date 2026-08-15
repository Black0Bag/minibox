// Package scheduler 提供调度中枢的 in-process 实现（robfig/cron v3）。
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

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
	runner scheduler.Runner
	logger *slog.Logger
	// id 计数器
	nextID int
}

// New 创建调度器。
func New(runner scheduler.Runner, logger *slog.Logger) *CronScheduler {
	return &CronScheduler{
		cron:   cron.New(cron.WithSeconds()),
		tasks:  make(map[string]scheduler.Task),
		entry:  make(map[string]cron.EntryID),
		runner: runner,
		logger: logger,
	}
}

// Add 注册任务。
func (s *CronScheduler) Add(task scheduler.Task) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 分配任务 ID
	s.nextID++
	id := fmt.Sprintf("task_%d", s.nextID)
	task.ID = id

	// 注册到 cron
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

// Stop 停止调度。
func (s *CronScheduler) Stop() {
	s.cron.Stop()
}

// runTask 执行任务（带预算 + panic 隔离）。
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

	// 预算墙钟限制
	ctx := context.Background()
	var cancel context.CancelFunc
	if task.Budget.MaxWall > 0 {
		ctx, cancel = context.WithTimeout(ctx, task.Budget.MaxWall)
		defer cancel()
	}

	// 异步执行（不阻塞 cron 主循环）
	go func() {
		result, tokens, err := s.runner.Run(ctx, task)
		if err != nil {
			s.logger.Warn("调度任务执行失败", "id", task.ID, "err", err)
			return
		}
		s.logger.Info("调度任务完成", "id", task.ID, "tokens", tokens, "result_len", len(result))
	}()
}
