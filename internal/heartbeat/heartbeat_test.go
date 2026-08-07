package heartbeat

import (
	"context"
	"testing"
	"time"
)

// fakeExec 记录心跳执行并返回结果
type fakeExec struct {
	calls   int
	lastArg string
}

func (f *fakeExec) Execute(_ context.Context, task Task) (string, error) {
	f.calls++
	f.lastArg = task.Prompt
	return "执行结果:" + task.Prompt, nil
}

func TestCreateTaskValidation(t *testing.T) {
	h := NewEngine(t.TempDir())
	if _, err := h.Create(Task{Desc: "", Interval: 300}); err == nil {
		t.Error("空描述应报错")
	}
	if _, err := h.Create(Task{Desc: "任务", Interval: 0}); err == nil {
		t.Error("间隔为 0 应报错")
	}
	if _, err := h.Create(Task{Desc: "任务", Interval: 10}); err == nil {
		t.Error("间隔小于最小值应报错")
	}
}

func TestCreateAndLoad(t *testing.T) {
	dir := t.TempDir()
	h := NewEngine(dir)
	tk, err := h.Create(Task{Desc: "清理临时文件", Interval: 3600, Prompt: "请清理临时文件", Boundaries: "只删除 /tmp 下的临时文件"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if tk.ID == "" {
		t.Error("应生成 ID")
	}
	h2 := NewEngine(dir)
	got, err := h2.Get(tk.ID)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if got.Boundaries != "只删除 /tmp 下的临时文件" {
		t.Errorf("边界丢失: %+v", got)
	}
}

func TestSubscribeAndDue(t *testing.T) {
	h := NewEngine(t.TempDir())
	// 刚创建：已订阅但未到间隔
	tk, _ := h.Create(Task{Desc: "心跳A", Interval: 300})
	// 手动设置 lastRun 到过去 → 到期
	if err := h.setLastRun(tk.ID, time.Now().Add(-310*time.Second)); err != nil {
		t.Fatalf("设置 lastRun 失败: %v", err)
	}
	due, err := h.DueTasks(time.Now())
	if err != nil {
		t.Fatalf("DueTasks 失败: %v", err)
	}
	if len(due) != 1 {
		t.Errorf("应 1 个到期任务，得到 %d", len(due))
	}
}

func TestRunDue(t *testing.T) {
	h := NewEngine(t.TempDir())
	tk, _ := h.Create(Task{Desc: "心跳B", Interval: 300, Prompt: "检查磁盘空间"})
	_ = h.setLastRun(tk.ID, time.Now().Add(-310*time.Second))
	exec := &fakeExec{}
	h.SetExecutor(exec)
	ctx := context.Background()
	n, err := h.RunDue(ctx, time.Now())
	if err != nil {
		t.Fatalf("RunDue 失败: %v", err)
	}
	if n != 1 {
		t.Errorf("应执行 1 个，得到 %d", n)
	}
	if exec.calls != 1 {
		t.Errorf("应调用执行器 1 次，得到 %d", exec.calls)
	}
	if exec.lastArg != "检查磁盘空间" {
		t.Errorf("传入提示错误: %s", exec.lastArg)
	}
	// 执行后不应再到期
	due, _ := h.DueTasks(time.Now())
	if len(due) != 0 {
		t.Errorf("执行后不应到期: %d", len(due))
	}
}

func TestUnsubscribe(t *testing.T) {
	h := NewEngine(t.TempDir())
	tk, _ := h.Create(Task{Desc: "心跳C", Interval: 300})
	if err := h.Unsubscribe(tk.ID); err != nil {
		t.Fatalf("退订失败: %v", err)
	}
	got, _ := h.Get(tk.ID)
	if got.Status != StatusOff {
		t.Errorf("退订后应 off，得到 %s", got.Status)
	}
}

func TestBoundariesPassed(t *testing.T) {
	h := NewEngine(t.TempDir())
	tk, _ := h.Create(Task{Desc: "心跳D", Interval: 300, Boundaries: "不触碰用户文件"})
	got, _ := h.Get(tk.ID)
	if got.Boundaries != "不触碰用户文件" {
		t.Errorf("边界丢失: %s", got.Boundaries)
	}
}

func TestList(t *testing.T) {
	h := NewEngine(t.TempDir())
	_, _ = h.Create(Task{Desc: "a", Interval: 3600})
	_, _ = h.Create(Task{Desc: "b", Interval: 7200})
	list, err := h.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("应有 2 任务，得到 %d", len(list))
	}
}
