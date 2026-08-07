package scheduler

import (
	"context"
	"testing"
	"time"
)

// fakeKBSink 记录写入知识库的调用
type fakeKBSink struct {
	calls []string
}

func (f *fakeKBSink) Write(zone, title, content string) {
	f.calls = append(f.calls, title+":"+content)
}

func TestCreateTaskValidation(t *testing.T) {
	s := NewScheduler(t.TempDir())
	// 时间为空 → 报错
	if _, err := s.Create(Task{Type: TypeAlarm, Time: ""}); err == nil {
		t.Error("时间为空应报错")
	}
	// 非法类型 → 报错
	if _, err := s.Create(Task{Type: "bad", Time: "12:00", Desc: "x"}); err == nil {
		t.Error("非法类型应报错")
	}
	// 内容为空 → 报错
	if _, err := s.Create(Task{Type: TypeAlarm, Time: "12:00", Desc: ""}); err == nil {
		t.Error("内容为空应报错")
	}
}

func TestCreateAndLoadTask(t *testing.T) {
	s := NewScheduler(t.TempDir())
	tk, err := s.Create(Task{Type: TypeAlarm, Time: "12:30", Desc: "午休提醒"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if tk.ID == "" {
		t.Error("应生成 ID")
	}
	// 重新加载（持久化）
	s2 := NewScheduler(s.dir) // 复用目录
	got, err := s2.Get(tk.ID)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if got.Desc != "午休提醒" {
		t.Errorf("持久化丢失: %+v", got)
	}
}

func TestDueTasks(t *testing.T) {
	s := NewScheduler(t.TempDir())
	// 已到期的任务（时间在过去）
	_, _ = s.Create(Task{Type: TypeAlarm, Time: "00:01", Desc: "已过期"})
	// 未到期任务（时间在未来）
	future := time.Now().Add(2 * time.Hour).Format("15:04")
	_, _ = s.Create(Task{Type: TypeAlarm, Time: future, Desc: "未到期"})

	due, err := s.DueTasks(time.Now())
	if err != nil {
		t.Fatalf("DueTasks 失败: %v", err)
	}
	if len(due) != 1 {
		t.Errorf("应 1 个到期任务，得到 %d", len(due))
	}
	if len(due) > 0 && due[0].Desc != "已过期" {
		t.Errorf("到期任务应为已过期: %s", due[0].Desc)
	}
}

func TestMarkDone(t *testing.T) {
	s := NewScheduler(t.TempDir())
	tk, _ := s.Create(Task{Type: TypeAlarm, Time: "00:01", Desc: "x"})
	taskID := tk.ID
	if err := s.MarkDone(taskID); err != nil {
		t.Fatalf("MarkDone 失败: %v", err)
	}
	got, _ := s.Get(taskID)
	if got.Status != StatusDone {
		t.Errorf("状态应为 done，得到 %s", got.Status)
	}
	// done 任务不应再到期
	due, _ := s.DueTasks(time.Now())
	for _, tk := range due {
		if tk.ID == taskID {
			t.Error("done 任务不应到期")
		}
	}
}

func TestRunDueTasksWritesKB(t *testing.T) {
	s := NewScheduler(t.TempDir())
	kb := &fakeKBSink{}
	s.SetKBSink(kb)
	_, _ = s.Create(Task{Type: TypeAlarm, Time: "00:01", Desc: "提醒事项", Payload: "去开会"})

	ctx := context.Background()
	n, err := s.RunDue(ctx, time.Now())
	if err != nil {
		t.Fatalf("RunDue 失败: %v", err)
	}
	if n != 1 {
		t.Errorf("应执行 1 个任务，得到 %d", n)
	}
	if len(kb.calls) != 1 {
		t.Fatalf("应写知识库 1 次，得到 %d", len(kb.calls))
	}
	if !containsStr(kb.calls[0], "提醒事项") || !containsStr(kb.calls[0], "去开会") {
		t.Errorf("知识库内容异常: %s", kb.calls[0])
	}
	// 执行后应 done
	due, _ := s.DueTasks(time.Now())
	if len(due) != 0 {
		t.Errorf("执行后不应有到期任务: %d", len(due))
	}
}

func TestCancelTask(t *testing.T) {
	s := NewScheduler(t.TempDir())
	tk, _ := s.Create(Task{Type: TypeAlarm, Time: "00:01", Desc: "x"})
	if err := s.Cancel(tk.ID); err != nil {
		t.Fatalf("Cancel 失败: %v", err)
	}
	got, _ := s.Get(tk.ID)
	if got.Status != StatusCancelled {
		t.Errorf("状态应为 cancelled，得到 %s", got.Status)
	}
}

func TestListTasks(t *testing.T) {
	s := NewScheduler(t.TempDir())
	_, _ = s.Create(Task{Type: TypeAlarm, Time: "00:01", Desc: "a"})
	_, _ = s.Create(Task{Type: TypeSchedule, Time: "00:02", Desc: "b"})
	_, _ = s.Create(Task{Type: TypeCalendar, Time: "00:03", Desc: "c"})
	tasks, err := s.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("应有 3 任务，得到 %d", len(tasks))
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
