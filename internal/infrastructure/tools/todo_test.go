package tools

import (
	"context"
	"encoding/json"
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

	// 创建一个带有 Run 的上下文
	ctx := context.Background()
	// 由于 getRunFromContext 还未实现，我们直接调用工具函数
	// 这里测试的是工具逻辑本身

	input := `{"content":"测试任务1"}`
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}

	// 直接调用工具函数（跳过上下文获取）
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

	// 创建一个带有任务的 Run
	run := &agent.Run{
		TodoItems: []agent.TodoItem{
			{
				ID:        "test-id-1",
				Content:   "测试任务1",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
		},
	}

	ctx := context.Background()

	// 更新任务状态为 in_progress
	input := `{"id":"test-id-1","status":"in_progress"}`
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}

	result, err := tool.(*todoTool).fn(ctx, run, args)
	if err != nil {
		t.Fatalf("更新任务失败: %v", err)
	}

	item := run.TodoItems[0]
	if item.Status != agent.TodoInProgress {
		t.Errorf("任务状态错误: 期望 'in_progress'，实际 '%s'", item.Status)
	}

	if item.StartedAt.IsZero() {
		t.Error("任务开始时间不应为空")
	}

	t.Logf("更新任务成功: %s", result)

	// 更新任务状态为 completed
	input = `{"id":"test-id-1","status":"completed"}`
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}

	result, err = tool.(*todoTool).fn(ctx, run, args)
	if err != nil {
		t.Fatalf("更新任务失败: %v", err)
	}

	item = run.TodoItems[0]
	if item.Status != agent.TodoCompleted {
		t.Errorf("任务状态错误: 期望 'completed'，实际 '%s'", item.Status)
	}

	if item.CompletedAt.IsZero() {
		t.Error("任务完成时间不应为空")
	}

	t.Logf("更新任务成功: %s", result)
}

// TestTodoList 测试列出所有 todo 任务项。
func TestTodoList(t *testing.T) {
	tool := NewTodoList()

	// 创建一个带有任务的 Run
	run := &agent.Run{
		TodoItems: []agent.TodoItem{
			{
				ID:        "test-id-1",
				Content:   "测试任务1",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
			{
				ID:        "test-id-2",
				Content:   "测试任务2",
				Status:    agent.TodoInProgress,
				CreatedAt: time.Now(),
				StartedAt: time.Now(),
			},
			{
				ID:          "test-id-3",
				Content:     "测试任务3",
				Status:      agent.TodoCompleted,
				CreatedAt:   time.Now(),
				StartedAt:   time.Now(),
				CompletedAt: time.Now(),
			},
		},
	}

	ctx := context.Background()

	input := `{}`
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("参数解析失败: %v", err)
	}

	result, err := tool.(*todoTool).fn(ctx, run, args)
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

	// 创建一个带有任务的 Run
	run := &agent.Run{
		TodoItems: []agent.TodoItem{
			{
				ID:        "test-id-1",
				Content:   "测试任务1",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
		},
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
			{
				ID:        "test-id-1",
				Content:   "测试任务1",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
			{
				ID:        "test-id-2",
				Content:   "测试任务2",
				Status:    agent.TodoInProgress,
				CreatedAt: time.Now(),
				StartedAt: time.Now(),
			},
		},
	}

	manager := NewTodoManager(run)

	// 测试获取下一个待处理任务
	nextPending := manager.GetNextPending()
	if nextPending == nil {
		t.Fatal("GetNextPending 不应返回 nil")
	}
	if nextPending.ID != "test-id-1" {
		t.Errorf("GetNextPending 返回错误的任务: 期望 'test-id-1'，实际 '%s'", nextPending.ID)
	}

	// 测试获取当前正在执行的任务
	currentInProgress := manager.GetCurrentInProgress()
	if currentInProgress == nil {
		t.Fatal("GetCurrentInProgress 不应返回 nil")
	}
	if currentInProgress.ID != "test-id-2" {
		t.Errorf("GetCurrentInProgress 返回错误的任务: 期望 'test-id-2'，实际 '%s'", currentInProgress.ID)
	}

	// 测试标记任务为进行中
	err := manager.MarkInProgress("test-id-1")
	if err != nil {
		t.Fatalf("MarkInProgress 失败: %v", err)
	}
	if run.TodoItems[0].Status != agent.TodoInProgress {
		t.Errorf("MarkInProgress 后状态错误: 期望 'in_progress'，实际 '%s'", run.TodoItems[0].Status)
	}

	// 测试标记任务为已完成
	err = manager.MarkCompleted("test-id-2")
	if err != nil {
		t.Fatalf("MarkCompleted 失败: %v", err)
	}
	if run.TodoItems[1].Status != agent.TodoCompleted {
		t.Errorf("MarkCompleted 后状态错误: 期望 'completed'，实际 '%s'", run.TodoItems[1].Status)
	}

	// 测试获取所有任务
	all := manager.GetAll()
	if len(all) != 2 {
		t.Errorf("GetAll 返回任务数量错误: 期望 2，实际 %d", len(all))
	}

	// 测试获取进度统计
	completed, total := manager.GetProgress()
	if total != 2 {
		t.Errorf("GetProgress total 错误: 期望 2，实际 %d", total)
	}
	if completed != 1 {
		t.Errorf("GetProgress completed 错误: 期望 1，实际 %d", completed)
	}
}
