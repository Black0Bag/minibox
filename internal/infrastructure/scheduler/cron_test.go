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
		Name: "备份", Spec: "0 * * * * ?", Type: scheduler.TypeSchedule,
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
	_, err := s.Add(scheduler.Task{Name: "坏", Spec: "not-a-cron"})
	if err == nil {
		t.Fatal("非法 cron 应报错")
	}
}

// TestRemove 移除任务。
func TestRemove(t *testing.T) {
	s := New(nil, slog.New(slog.DiscardHandler))
	id, _ := s.Add(scheduler.Task{Name: "x", Spec: "0 * * * * ?"})
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
	_, _ = s.Add(scheduler.Task{Name: "tick", Spec: "*/1 * * * * ?"})

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
