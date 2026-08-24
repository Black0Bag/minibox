package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
)

// TestTodoCreate 测试创建 todo 任务。
func TestTodoCreate(t *testing.T) {
	tool := NewTodoCreate()
	
	// 创建一个模拟的 Run
	run := &agent.Run{
		TodoItems: []agent.TodoItem{},
	}
	
	ctx := context.Background()
	
	input := `{"content":"测试任务1"}`
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}
	
	result, err := tool.(*todoTool).fn(ctx, run, args)
	if err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}
	
	if len(run.TodoItems) != 1 {
		t.Fatalf("预期1个任务，实际有 %d 个", len(run.TodoItems))
	}
	
	item := run.TodoItems[0]
	if item.Content != "测试任务1" {
		t.Errorf("任务内容错误: 期望 '测试任务1'，实际 '%s'", item.Content)
	}
	
	if item.Status != agent.TodoPending {
		t.Errorf("任务状态错误: 期望 'pending'，实际 '%s'", item.Status)
	}
	
	if item.ID == "" {
		t.Error("任务ID不应为空")
	}
	
	t.Logf("创建任务成功: %s", result)
}

// TestTodoUpdate 测试更新 todo 任务状态。
func TestTodoUpdate(t *testing.T) {
	tool := NewTodoUpdate()
	
	run := &agent.Run{
		TodoItems: []agent.TodoItem{{
			ID: "test-id-1", Content: "测试任务1", Status: agent.TodoPending, CreatedAt: time.Now(),
		}},
	}
	
	ctx := context.Background()
	
	input := `{"id":"test-id-1","status":"in_progress"}`
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}
	
	result, err := tool.(*todoTool).fn(ctx, run, args)
	if err != nil {
		t.Fatalf("更新任务失败: %v", err)
	}
	
	if run.TodoItems[0].Status != agent.TodoInProgress {
		t.Errorf("任务状态错误: 期望 'in_progress'，实际 '%s'", run.TodoItems[0].Status)
	}
	
	t.Logf("更新任务成功: %s", result)
}

// TestTodoList 测试列出所有 todo 任务项。
func TestTodoList(t *testing.T) {
	tool := NewTodoList()
	
	run := &agent.Run{
		TodoItems: []agent.TodoItem{
			{ID: "test-id-1", Content: "任务1", Status: agent.TodoPending, CreatedAt: time.Now()},
			{ID: "test-id-2", Content: "任务2", Status: agent.TodoInProgress, CreatedAt: time.Now()},
			{ID: "test-id-3", Content: "任务3", Status: agent.TodoCompleted, CreatedAt: time.Now()},
		},
	}
	
	ctx := context.Background()
	result, err := tool.(*todoTool).fn(ctx, run, nil)
	if err != nil {
		t.Fatalf("列出任务失败: %v", err)
	}
	
	if len(result) == 0 {
		t.Error("结果不应为空")
	}
	
	t.Logf("列出任务成功:\n%s", result)
}

// TestTodoDelete 测试删除 todo 任务项。
func TestTodoDelete(t *testing.T) {
	tool := NewTodoDelete()
	
	run := &agent.Run{
		TodoItems: []agent.TodoItem{{
			ID: "test-id-1", Content: "测试任务1", Status: agent.TodoPending, CreatedAt: time.Now(),
		}},
	}
	
	ctx := context.Background()
	input := `{"id":"test-id-1"}`
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}
	
	result, err := tool.(*todoTool).fn(ctx, run, args)
	if err != nil {
		t.Fatalf("删除任务失败: %v", err)
	}
	
	if len(run.TodoItems) != 0 {
		t.Fatalf("预期0个任务，实际有 %d 个", len(run.TodoItems))
	}
	
	t.Logf("删除任务成功: %s", result)
}

// TestTodoManager 测试 TodoManager 辅助功能。
func TestTodoManager(t *testing.T) {
	run := &agent.Run{
		TodoItems: []agent.TodoItem{
			{ID: "task-1", Content: "任务1", Status: agent.TodoPending, CreatedAt: time.Now()},
			{ID: "task-2", Content: "任务2", Status: agent.TodoInProgress, CreatedAt: time.Now(), StartedAt: time.Now()},
		},
	}
	
	manager := NewTodoManager(run)
	
	nextPending := manager.GetNextPending()
	if nextPending == nil {
		t.Fatal("GetNextPending 不应返回 nil")
	}
	if nextPending.ID != "task-1" {
		t.Errorf("GetNextPending 返回错误: 期望 'task-1'，实际 '%s'", nextPending.ID)
	}
	
	currentInProgress := manager.GetCurrentInProgress()
	if currentInProgress == nil {
		t.Fatal("GetCurrentInProgress 不应返回 nil")
	}
	if currentInProgress.ID != "task-2" {
		t.Errorf("GetCurrentInProgress 返回错误: 期望 'task-2'，实际 '%s'", currentInProgress.ID)
	}
	
	err := manager.MarkInProgress("task-1")
	if err != nil {
		t.Fatalf("MarkInProgress 失败: %v", err)
	}
	if run.TodoItems[0].Status != agent.TodoInProgress {
		t.Errorf("MarkInProgress 后状态错误: 期望 'in_progress'，实际 '%s'", run.TodoItems[0].Status)
	}
	
	err = manager.MarkCompleted("task-2")
	if err != nil {
		t.Fatalf("MarkCompleted 失败: %v", err)
	}
	if run.TodoItems[1].Status != agent.TodoCompleted {
		t.Errorf("MarkCompleted 后状态错误: 期望 'completed'，实际 '%s'", run.TodoItems[1].Status)
	}
	
	all := manager.GetAll()
	if len(all) != 2 {
		t.Errorf("GetAll 返回任务数量错误: 期望 2，实际 %d", len(all))
	}
	
	completed, total := manager.GetProgress()
	if total != 2 {
		t.Errorf("GetProgress total 错误: 期望 2，实际 %d", total)
	}
	if completed != 1 {
		t.Errorf("GetProgress completed 错误: 期望 1，实际 %d", completed)
	}
}