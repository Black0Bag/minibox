package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/subagent"
)

// mockSubAgent 测试用假 subagent。
type mockSubAgent struct {
	id    string
	fail  bool
	delay time.Duration
	ran   bool
}

func (m *mockSubAgent) ID() string { return m.id }

func (m *mockSubAgent) Run(_ context.Context, t subagent.Task) (subagent.Result, error) {
	m.ran = true
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.fail {
		return subagent.Result{}, errors.New("subagent 失败")
	}
	return subagent.Result{
		AgentID: m.id,
		Content: "结果-" + m.id,
		Tokens:  t.MaxTokens,
		Latency: time.Millisecond,
	}, nil
}

var _ subagent.Agent = (*mockSubAgent)(nil)

// TestDispatchParallel 验证并行 fan-out + 失败是 value。
func TestDispatchParallel(t *testing.T) {
	o := NewOrchestrator(subagent.Config{MaxConcurrent: 3, AgentTimeout: 10 * time.Second}, testLogger{})

	a1 := &mockSubAgent{id: "a1"}
	a2 := &mockSubAgent{id: "a2", fail: true} // 这个失败，不影响其他
	a3 := &mockSubAgent{id: "a3"}
	o.Register(a1)
	o.Register(a2)
	o.Register(a3)

	plan := []subagent.Task{
		{AgentID: "a1", Objective: "任务1", MaxTokens: 100},
		{AgentID: "a2", Objective: "任务2", MaxTokens: 100},
		{AgentID: "a3", Objective: "任务3", MaxTokens: 100},
	}

	report, err := o.Dispatch(context.Background(), plan)
	if err != nil {
		t.Fatalf("Dispatch 失败: %v", err)
	}

	// 2 个成功，1 个失败（失败是 value，不杀兄弟）
	if len(report.Results) != 2 {
		t.Errorf("应有 2 个成功结果，实际 %d", len(report.Results))
	}
	if len(report.Failures) != 1 {
		t.Errorf("应有 1 个失败，实际 %d", len(report.Failures))
	}
	if report.Failures[0].AgentID != "a2" {
		t.Errorf("失败应是 a2，实际 %s", report.Failures[0].AgentID)
	}
}

// TestDispatchBudget 验证预算限制。
func TestDispatchBudget(t *testing.T) {
	o := NewOrchestrator(subagent.Config{
		MaxConcurrent: 3,
		AgentTimeout:  10 * time.Second,
		TokenBudget:   250, // 预算 250，3 个任务各 100 = 300 超限
	}, testLogger{})

	o.Register(&mockSubAgent{id: "a1"})
	o.Register(&mockSubAgent{id: "a2"})
	o.Register(&mockSubAgent{id: "a3"})

	plan := []subagent.Task{
		{AgentID: "a1", Objective: "任务1", MaxTokens: 100},
		{AgentID: "a2", Objective: "任务2", MaxTokens: 100},
		{AgentID: "a3", Objective: "任务3", MaxTokens: 100},
	}

	report, err := o.Dispatch(context.Background(), plan)
	if err != nil {
		t.Fatalf("Dispatch 失败: %v", err)
	}

	// 预算 250，最多 2 个任务能通过（200），第 3 个预算超限
	if len(report.Results) > 2 {
		t.Errorf("预算限制失效，成功 %d 个（应 ≤2）", len(report.Results))
	}
}

// TestRunChain 验证串行执行。
func TestRunChain(t *testing.T) {
	o := NewOrchestrator(subagent.Config{AgentTimeout: 10 * time.Second}, testLogger{})
	o.Register(&mockSubAgent{id: "step1"})
	o.Register(&mockSubAgent{id: "step2"})

	report, err := o.RunChain(context.Background(), []subagent.Task{
		{AgentID: "step1", Objective: "第一步"},
		{AgentID: "step2", Objective: "第二步"},
	})
	if err != nil {
		t.Fatalf("RunChain 失败: %v", err)
	}
	if len(report.Results) != 2 {
		t.Errorf("串行应有 2 个结果，实际 %d", len(report.Results))
	}
}

// TestRunSingleNotFound 验证未注册 subagent。
func TestRunSingleNotFound(t *testing.T) {
	o := NewOrchestrator(subagent.Config{AgentTimeout: 10 * time.Second}, testLogger{})
	_, err := o.RunSingle(context.Background(), subagent.Task{AgentID: "不存在"})
	if err == nil {
		t.Error("未注册 subagent 应报错")
	}
}

// TestRunBackground 验证后台执行。
func TestRunBackground(t *testing.T) {
	o := NewOrchestrator(subagent.Config{AgentTimeout: 10 * time.Second}, testLogger{})
	o.Register(&mockSubAgent{id: "bg", delay: 50 * time.Millisecond})

	ch, err := o.RunBackground(context.Background(), subagent.Task{AgentID: "bg", Objective: "后台任务"})
	if err != nil {
		t.Fatalf("RunBackground 失败: %v", err)
	}

	select {
	case res := <-ch:
		if res.AgentID != "bg" {
			t.Errorf("后台结果错误: %s", res.AgentID)
		}
	case <-time.After(2 * time.Second):
		t.Error("后台执行超时")
	}
}
