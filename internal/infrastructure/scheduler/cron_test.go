package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/scheduler"
)

// mockRunner 记录被调用的任务。
type mockRunner struct {
	mu      sync.Mutex
	called  []string
	results map[string]string
}

func (m *mockRunner) Run(_ context.Context, task scheduler.Task) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = append(m.called, task.ID)
	if m.results == nil {
		m.results = make(map[string]string)
	}
	m.results[task.ID] = "result:" + task.Name
	return m.results[task.ID], 10, nil
}

var _ scheduler.Runner = (*mockRunner)(nil)

// TestAdd_List 添加任务 + 列出。
func TestAdd_List(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	id, err := s.Add(scheduler.Task{
		Name: "备份", Spec: "0 * * * * ?", Type: scheduler.TypeSchedule, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if id == "" {
		t.Fatal("任务 ID 为空")
	}
	tasks := s.List()
	if len(tasks) != 1 {
		t.Fatalf("应 1 个任务，实际 %d", len(tasks))
	}
	if tasks[0].Name != "备份" {
		t.Errorf("任务名=%q", tasks[0].Name)
	}
}

// TestAdd_InvalidCron 非法 cron 表达式。
func TestAdd_InvalidCron(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	_, err := s.Add(scheduler.Task{Name: "坏", Spec: "not-a-cron", Enabled: true})
	if err == nil {
		t.Fatal("非法 cron 应报错")
	}
}

// TestAdd_Disabled 未启用任务拒绝注册。
func TestAdd_Disabled(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	if _, err := s.Add(scheduler.Task{Name: "off", Spec: "0 * * * * ?"}); err == nil {
		t.Fatal("Enabled=false 应拒绝注册")
	}
}

// TestRemove 移除任务。
func TestRemove(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	id, _ := s.Add(scheduler.Task{Name: "x", Spec: "0 * * * * ?", Enabled: true})
	if err := s.Remove(id); err != nil {
		t.Fatalf("Remove err=%v", err)
	}
	if err := s.Remove(id); err == nil {
		t.Fatal("重复移除应报错")
	}
	if len(s.List()) != 0 {
		t.Fatal("移除后应无任务")
	}
}

// TestRun 任务触发时调用 runner（用极短周期的 cron 验证）。
func TestRun(t *testing.T) {
	runner := &mockRunner{}
	s := New(runner, slog.New(slog.DiscardHandler))
	// 每 100ms 触发一次（cron WithSeconds）
	_, _ = s.Add(scheduler.Task{Name: "tick", Spec: "*/1 * * * * ?", Type: scheduler.TypeSchedule, Enabled: true})

	s.cron.Start()
	defer s.cron.Stop()

	// 等待至少一次触发
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runner.mu.Lock()
		n := len(runner.called)
		runner.mu.Unlock()
		if n > 0 {
			runner.mu.Lock()
			res := runner.results[runner.called[0]]
			runner.mu.Unlock()
			if res == "" {
				t.Fatal("runner 结果未写回")
			}
			return // 成功触发
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("任务应在 3s 内触发")
}

// TestRun_WallBudget 预算墙钟：慢任务超时被取消，runner 收到 ctx.Err()。
func TestRun_WallBudget(t *testing.T) {
	runner := &budgetRunner{delay: 3 * time.Second}
	s := New(runner, slog.New(slog.DiscardHandler))
	// 每次触发耗时 3s > 预算 500ms → 被取消
	_, _ = s.Add(scheduler.Task{
		Name: "slow", Spec: "*/1 * * * * ?", Type: scheduler.TypeSchedule, Enabled: true,
		Budget: scheduler.Budget{MaxWall: 500 * time.Millisecond},
	})

	s.cron.Start()
	defer s.cron.Stop()

	if !runner.waitResult(5 * time.Second) {
		t.Fatal("任务应在 5s 内执行完（被预算取消）")
	}
	if !runner.wasCancelled() {
		t.Error("慢任务应被预算墙钟取消（ctx.Err() == DeadlineExceeded）")
	}
}

// budgetRunner 记录任务是否被 ctx 取消。
type budgetRunner struct {
	mu     sync.Mutex
	done   bool
	cancel bool
	delay  time.Duration
}

func (b *budgetRunner) Run(ctx context.Context, _ scheduler.Task) (string, int, error) {
	select {
	case <-ctx.Done():
		b.mu.Lock()
		b.cancel = ctx.Err() != nil
		b.done = true
		b.mu.Unlock()
		return "", 0, ctx.Err()
	case <-time.After(b.delay):
		b.mu.Lock()
		b.done = true
		b.mu.Unlock()
		return "ok", 1, nil
	}
}

func (b *budgetRunner) waitResult(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		done := b.done
		b.mu.Unlock()
		if done {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (b *budgetRunner) wasCancelled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cancel
}

var _ scheduler.Runner = (*budgetRunner)(nil)

// TestRun_NoOverlap SkipIfStillRunning：慢任务运行中跳过后续触发，不叠加。
func TestRun_NoOverlap(t *testing.T) {
	// 每 200ms 触发，任务执行 700ms → 若重叠保护失效会并发；有效则跳过。
	// 统计同一时刻并发调用数，断言峰值 ≤1。
	runner := &overlapRunner{}
	s := New(runner, slog.New(slog.DiscardHandler))
	_, _ = s.Add(scheduler.Task{Name: "slow", Spec: "*/1 * * * * ?", Type: scheduler.TypeSchedule, Enabled: true})

	s.cron.Start()
	defer s.cron.Stop()

	if !runner.waitObserved(6 * time.Second) {
		t.Fatal("任务应至少观察一次执行")
	}
	if runner.maxConcurrent() > 1 {
		t.Errorf("同一任务不应并发执行，峰值并发 = %d", runner.maxConcurrent())
	}
}

// overlapRunner 统计并发执行峰值。
type overlapRunner struct {
	mu        sync.Mutex
	active    int
	maxActive int
	observed  bool
}

func (o *overlapRunner) Run(_ context.Context, _ scheduler.Task) (string, int, error) {
	o.mu.Lock()
	o.active++
	o.observed = true
	if o.active > o.maxActive {
		o.maxActive = o.active
	}
	o.mu.Unlock()

	time.Sleep(700 * time.Millisecond)

	o.mu.Lock()
	o.active--
	o.mu.Unlock()
	return "ok", 1, nil
}

func (o *overlapRunner) maxConcurrent() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.maxActive
}

func (o *overlapRunner) waitObserved(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		obs := o.observed
		o.mu.Unlock()
		if obs {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

var _ scheduler.Runner = (*overlapRunner)(nil)

// TestAlarm 单次闹钟：At 触发一次后自动移除。
func TestAlarm(t *testing.T) {
	runner := &mockRunner{}
	s := New(runner, slog.New(slog.DiscardHandler))
	_, err := s.Add(scheduler.Task{
		Name: "提醒", Type: scheduler.TypeAlarm, Enabled: true,
		At: func() *time.Time { t := time.Now().Add(300 * time.Millisecond); return &t }(),
	})
	if err != nil {
		t.Fatalf("Add alarm err=%v", err)
	}
	if len(s.List()) != 1 {
		t.Fatalf("注册后应 1 个任务")
	}
	// 等触发
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runner.mu.Lock()
		n := len(runner.called)
		runner.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(runner.called) == 0 {
		t.Fatal("闹钟应在 3s 内触发")
	}
	// 触发后自动移除
	time.Sleep(200 * time.Millisecond)
	if len(s.List()) != 0 {
		t.Errorf("闹钟触发后应自动移除，仍有 %d 个", len(s.List()))
	}
}

// TestAlarm_PastTime 闹钟时间已过期拒绝注册。
func TestAlarm_PastTime(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	past := time.Now().Add(-time.Second)
	_, err := s.Add(scheduler.Task{Name: "过期", Type: scheduler.TypeAlarm, Enabled: true, At: &past})
	if err == nil {
		t.Fatal("过期闹钟应报错")
	}
}

// TestAlarm_Remove 移除未触发的闹钟。
func TestAlarm_Remove(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	future := time.Now().Add(time.Hour)
	id, _ := s.Add(scheduler.Task{Name: "未来", Type: scheduler.TypeAlarm, Enabled: true, At: &future})
	if err := s.Remove(id); err != nil {
		t.Fatalf("Remove alarm err=%v", err)
	}
	if len(s.List()) != 0 {
		t.Error("移除后应无任务")
	}
}
