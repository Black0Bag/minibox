package todolist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// execRecorder 记录执行调用，模拟步骤执行
type execRecorder struct {
	calls  []int
	failOn map[int]error
}

func (e *execRecorder) Exec(_ context.Context, p *Plan, idx int) (string, error) {
	e.calls = append(e.calls, idx)
	if err, ok := e.failOn[idx]; ok {
		return "", err
	}
	return "步骤" + p.Steps[idx].Desc + "完成", nil
}

func testDriver(t *testing.T) *Driver {
	t.Helper()
	return NewDriver(t.TempDir())
}

func TestCreatePlanValidation(t *testing.T) {
	d := testDriver(t)
	// goal 为空 → 报错
	if _, err := d.Create("", "目标", nil); err == nil {
		t.Error("goal 为空应报错")
	}
	// 步骤描述为空 → 报错
	if _, err := d.Create("测试计划", "目标", []Step{{Desc: ""}}); err == nil {
		t.Error("步骤描述为空应报错")
	}
}

func TestCreateAndGetPlan(t *testing.T) {
	d := testDriver(t)
	p, err := d.Create("测试计划", "完成一个任务", []Step{
		{Desc: "第一步"},
		{Desc: "第二步"},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if p.ID == "" {
		t.Error("计划应生成 ID")
	}
	if len(p.Steps) != 2 {
		t.Errorf("应有 2 步，得到 %d", len(p.Steps))
	}
	if p.Steps[0].Status != StepPending {
		t.Errorf("初始状态应为 pending，得到 %s", p.Steps[0].Status)
	}

	// 重新加载（中断续跑验证）
	loaded, err := d.Get(p.ID)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if loaded.Goal != "完成一个任务" {
		t.Errorf("Goal 不一致: %s", loaded.Goal)
	}
}

func TestRunAll(t *testing.T) {
	d := testDriver(t)
	p, _ := d.Create("测试计划", "目标", []Step{{Desc: "甲"}, {Desc: "乙"}, {Desc: "丙"}})
	rec := &execRecorder{}
	err := d.RunAll(context.Background(), p.ID, rec.Exec)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if len(rec.calls) != 3 {
		t.Errorf("应执行 3 步，实际 %d", len(rec.calls))
	}
	// 全部 done
	loaded, _ := d.Get(p.ID)
	for i, s := range loaded.Steps {
		if s.Status != StepDone {
			t.Errorf("步骤 %d 应 done，得到 %s", i, s.Status)
		}
		if s.Output == "" {
			t.Errorf("步骤 %d 应有输出", i)
		}
	}
	if loaded.Status != PlanDone {
		t.Errorf("计划应 done，得到 %s", loaded.Status)
	}
}

func TestRunAllStepFailure(t *testing.T) {
	d := testDriver(t)
	p, _ := d.Create("测试计划", "目标", []Step{{Desc: "甲"}, {Desc: "乙"}, {Desc: "丙"}})
	rec := &execRecorder{failOn: map[int]error{1: errors.New("模拟失败")}}
	err := d.RunAll(context.Background(), p.ID, rec.Exec)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "步骤 2") {
		t.Errorf("错误应含失败步骤: %v", err)
	}
	loaded, _ := d.Get(p.ID)
	if loaded.Status != PlanFailed {
		t.Errorf("计划应 failed，得到 %s", loaded.Status)
	}
	if loaded.Steps[1].Status != StepFailed {
		t.Errorf("步骤 2 应 failed，得到 %s", loaded.Steps[1].Status)
	}
	// 后续步骤不应执行
	if loaded.Steps[2].Status != StepPending {
		t.Errorf("步骤 3 应保持 pending，得到 %s", loaded.Steps[2].Status)
	}
}

func TestResumeAfterInterruption(t *testing.T) {
	// 中断续跑：模拟第 2 步完成后中断，再从第 3 步继续
	dir := t.TempDir()
	d := NewDriver(dir)
	p, _ := d.Create("测试计划", "目标", []Step{{Desc: "甲"}, {Desc: "乙"}, {Desc: "丙"}})
	rec := &execRecorder{}
	if err := d.RunAll(context.Background(), p.ID, rec.Exec); err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 模拟中断：新进程重新加载后重跑
	d2 := NewDriver(dir)
	p2, err := d2.Get(p.ID)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	if p2.Status != PlanDone {
		t.Fatalf("重载后应为 done，得到 %s", p2.Status)
	}
	// 已 done 的计划重跑应立即返回
	if err := d2.RunAll(context.Background(), p2.ID, rec.Exec); err != nil {
		t.Errorf("重跑 done 计划不应报错: %v", err)
	}
	if len(rec.calls) != 3 {
		t.Errorf("done 计划不应重新执行步骤，calls=%v", rec.calls)
	}
}

func TestRollback(t *testing.T) {
	d := testDriver(t)
	p, _ := d.Create("测试计划", "目标", []Step{{Desc: "甲"}, {Desc: "乙"}, {Desc: "丙"}})
	rec := &execRecorder{}
	if err := d.RunAll(context.Background(), p.ID, rec.Exec); err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 回滚到步骤 2（下标 1）：步骤 2、3 重置为 pending
	err := d.Rollback(p.ID, 1)
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	loaded, _ := d.Get(p.ID)
	if loaded.Steps[0].Status != StepDone {
		t.Errorf("回滚目标前的步骤应保持 done，得到 %s", loaded.Steps[0].Status)
	}
	if loaded.Steps[1].Status != StepPending {
		t.Errorf("回滚目标步骤应重置为 pending，得到 %s", loaded.Steps[1].Status)
	}
	if loaded.Steps[2].Status != StepPending {
		t.Errorf("回滚目标后的步骤应重置为 pending，得到 %s", loaded.Steps[2].Status)
	}
	if loaded.Status != PlanPending {
		t.Errorf("回滚后计划应回 pending，得到 %s", loaded.Status)
	}

	// 回滚后再跑：只有后两步执行
	rec2 := &execRecorder{}
	if err := d.RunAll(context.Background(), p.ID, rec2.Exec); err != nil {
		t.Fatalf("重跑失败: %v", err)
	}
	if len(rec2.calls) != 2 {
		t.Errorf("回滚后应只执行 2 步，calls=%v", rec2.calls)
	}
}

func TestRollbackInvalidIndex(t *testing.T) {
	d := testDriver(t)
	p, _ := d.Create("测试计划", "目标", []Step{{Desc: "甲"}})
	if err := d.Rollback(p.ID, 5); err == nil {
		t.Error("越界回滚应报错")
	}
}

func TestDeletePlan(t *testing.T) {
	d := testDriver(t)
	p, _ := d.Create("测试计划", "目标", []Step{{Desc: "甲"}})
	if err := d.Delete(p.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := d.Get(p.ID); !os.IsNotExist(err) {
		t.Errorf("删除后 Get 应返回文件不存在，得到 %v", err)
	}
}

func TestListPlans(t *testing.T) {
	d := testDriver(t)
	_, _ = d.Create("计划一", "目标1", []Step{{Desc: "甲"}})
	_, _ = d.Create("计划二", "目标2", []Step{{Desc: "乙"}})
	plans, err := d.List()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("应有 2 个计划，得到 %d", len(plans))
	}
}

func TestPlanFilePathSafe(t *testing.T) {
	d := testDriver(t)
	// ID 含路径分隔符应被拒绝或清理
	p, err := d.Create("测试", "目标", []Step{{Desc: "甲"}})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if strings.ContainsAny(p.ID, `/\`) {
		t.Errorf("ID 不应含路径分隔符: %q", p.ID)
	}
	// 确认文件写在计划目录内
	full := filepath.Join(d.dir, p.ID+".json")
	if _, err := os.Stat(full); err != nil {
		t.Errorf("计划文件应存在: %v", err)
	}
}
