package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/llm"
)

// TestTodoToolsIntegration 测试 todo 工具集成。
func TestTodoToolsIntegration(t *testing.T) {
	// 创建一个模拟的 Agent Run
	run := &agent.Run{
		ID:        "test-run-1",
		SessionID: "test-session-1",
		State:     agent.StatePlanning,
		Mode:      agent.ModeBuild,
		TodoItems: []agent.TodoItem{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 创建 todo 工具
	createTool := NewTodoCreate()
	updateTool := NewTodoUpdate()
	listTool := NewTodoList()
	deleteTool := NewTodoDelete()

	// 创建一个带有 Run 的上下文
	ctx := context.Background()
	ctx = context.WithValue(ctx, runContextKey{}, run)

	// 测试创建任务
	t.Run("CreateTask", func(t *testing.T) {
		input := []byte(`{"content":"测试任务1"}`)
		result, err := createTool.Invoke(ctx, input)
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
		
		t.Logf("创建任务成功: %s", result)
	})

	// 测试更新任务状态
	t.Run("UpdateTaskStatus", func(t *testing.T) {
		item := run.TodoItems[0]
		input := []byte(`{"id":"` + item.ID + `","status":"in_progress"}`)
		result, err := updateTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("更新任务状态失败: %v", err)
		}
		
		updatedItem := run.TodoItems[0]
		if updatedItem.Status != agent.TodoInProgress {
			t.Errorf("任务状态错误: 期望 'in_progress'，实际 '%s'", updatedItem.Status)
		}
		
		if updatedItem.StartedAt.IsZero() {
			t.Error("任务开始时间不应为空")
		}
		
		t.Logf("更新任务状态成功: %s", result)
	})

	// 测试列出任务
	t.Run("ListTasks", func(t *testing.T) {
		input := []byte(`{}`)
		result, err := listTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("列出任务失败: %v", err)
		}
		
		if len(result) == 0 {
			t.Error("结果不应为空")
		}
		
		t.Logf("列出任务成功:\n%s", result)
	})

	// 测试创建更多任务
	t.Run("CreateMoreTasks", func(t *testing.T) {
		input := []byte(`{"content":"测试任务2"}`)
		_, err := createTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("创建任务失败: %v", err)
		}
		
		input = []byte(`{"content":"测试任务3"}`)
		_, err = createTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("创建任务失败: %v", err)
		}
		
		if len(run.TodoItems) != 3 {
			t.Fatalf("预期3个任务，实际有 %d 个", len(run.TodoItems))
		}
		
		t.Log("创建更多任务成功")
	})

	// 测试标记任务完成
	t.Run("CompleteTask", func(t *testing.T) {
		item := run.TodoItems[0]
		input := []byte(`{"id":"` + item.ID + `","status":"completed"}`)
		result, err := updateTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("标记任务完成失败: %v", err)
		}
		
		completedItem := run.TodoItems[0]
		if completedItem.Status != agent.TodoCompleted {
			t.Errorf("任务状态错误: 期望 'completed'，实际 '%s'", completedItem.Status)
		}
		
		if completedItem.CompletedAt.IsZero() {
			t.Error("任务完成时间不应为空")
		}
		
		t.Logf("标记任务完成成功: %s", result)
	})

	// 测试列出所有任务
	t.Run("ListAllTasks", func(t *testing.T) {
		input := []byte(`{}`)
		result, err := listTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("列出任务失败: %v", err)
		}
		
		t.Logf("所有任务:\n%s", result)
	})

	// 测试删除任务
	t.Run("DeleteTask", func(t *testing.T) {
		item := run.TodoItems[1]
		input := []byte(`{"id":"` + item.ID + `","content":"测试任务2"}`)
		result, err := deleteTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("删除任务失败: %v", err)
		}
		
		if len(run.TodoItems) != 2 {
			t.Fatalf("预期2个任务，实际有 %d 个", len(run.TodoItems))
		}
		
		t.Logf("删除任务成功: %s", result)
	})

	// 测试最终状态
	t.Run("FinalState", func(t *testing.T) {
		input := []byte(`{}`)
		result, err := listTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("列出任务失败: %v", err)
		}
		
		t.Logf("最终任务状态:\n%s", result)
	})
}

// TestTodoManagerIntegration 测试 TodoManager 集成。
func TestTodoManagerIntegration(t *testing.T) {
	run := &agent.Run{
		ID:        "test-run-2",
		SessionID: "test-session-2",
		TodoItems: []agent.TodoItem{
			{
				ID:        "task-1",
				Content:   "任务1",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
			{
				ID:        "task-2",
				Content:   "任务2",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
			{
				ID:        "task-3",
				Content:   "任务3",
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	manager := NewTodoManager(run)

	// 测试获取下一个待处理任务
	t.Run("GetNextPending", func(t *testing.T) {
		next := manager.GetNextPending()
		if next == nil {
			t.Fatal("GetNextPending 不应返回 nil")
		}
		if next.ID != "task-1" {
			t.Errorf("GetNextPending 返回错误的任务: 期望 'task-1'，实际 '%s'", next.ID)
		}
	})

	// 测试标记任务为进行中
	t.Run("MarkInProgress", func(t *testing.T) {
		err := manager.MarkInProgress("task-1")
		if err != nil {
			t.Fatalf("MarkInProgress 失败: %v", err)
		}
		
		if run.TodoItems[0].Status != agent.TodoInProgress {
			t.Errorf("MarkInProgress 后状态错误: 期望 'in_progress'，实际 '%s'", run.TodoItems[0].Status)
		}
		
		if run.TodoItems[0].StartedAt.IsZero() {
			t.Error("MarkInProgress 后开始时间不应为空")
		}
	})

	// 测试标记任务为已完成
	t.Run("MarkCompleted", func(t *testing.T) {
		err := manager.MarkCompleted("task-1")
		if err != nil {
			t.Fatalf("MarkCompleted 失败: %v", err)
		}
		
		if run.TodoItems[0].Status != agent.TodoCompleted {
			t.Errorf("MarkCompleted 后状态错误: 期望 'completed'，实际 '%s'", run.TodoItems[0].Status)
		}
		
		if run.TodoItems[0].CompletedAt.IsZero() {
			t.Error("MarkCompleted 后完成时间不应为空")
		}
	})

	// 测试获取下一个待处理任务
	t.Run("GetNextPendingAfterCompletion", func(t *testing.T) {
		next := manager.GetNextPending()
		if next == nil {
			t.Fatal("GetNextPending 不应返回 nil")
		}
		if next.ID != "task-2" {
			t.Errorf("GetNextPending 返回错误的任务: 期望 'task-2'，实际 '%s'", next.ID)
		}
	})

	// 测试获取当前正在执行的任务
	t.Run("GetCurrentInProgress", func(t *testing.T) {
		// 标记 task-2 为进行中
		err := manager.MarkInProgress("task-2")
		if err != nil {
			t.Fatalf("MarkInProgress 失败: %v", err)
		}
		
		current := manager.GetCurrentInProgress()
		if current == nil {
			t.Fatal("GetCurrentInProgress 不应返回 nil")
		}
		if current.ID != "task-2" {
			t.Errorf("GetCurrentInProgress 返回错误的任务: 期望 'task-2'，实际 '%s'", current.ID)
		}
	})

	// 测试获取进度统计
	t.Run("GetProgress", func(t *testing.T) {
		completed, total := manager.GetProgress()
		if total != 3 {
			t.Errorf("GetProgress total 错误: 期望 3，实际 %d", total)
		}
		if completed != 1 {
			t.Errorf("GetProgress completed 错误: 期望 1，实际 %d", completed)
		}
	})

	// 测试获取所有任务
	t.Run("GetAll", func(t *testing.T) {
		all := manager.GetAll()
		if len(all) != 3 {
			t.Errorf("GetAll 返回任务数量错误: 期望 3，实际 %d", len(all))
		}
	})
}

// TestTodoWithEngineIntegration 测试与 Agent 引擎的集成。
func TestTodoWithEngineIntegration(t *testing.T) {
	// 创建一个模拟的 Agent Run
	run := &agent.Run{
		ID:        "test-run-3",
		SessionID: "test-session-3",
		State:     agent.StatePlanning,
		Mode:      agent.ModeBuild,
		TodoItems: []agent.TodoItem{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 创建一个带有 Run 的上下文
	ctx := context.Background()
	ctx = context.WithValue(ctx, runContextKey{}, run)

	// 模拟 LLM 返回 todo_create 工具调用
	todoCall := llm.ToolCall{
		ID:        "call-1",
		Name:      "todo_create",
		Arguments: `{"content":"LLM 生成的任务"}`,
	}

	// 创建工具并直接执行
	createTool := NewTodoCreate()
	result, err := createTool.Invoke(ctx, []byte(todoCall.Arguments))
	if err != nil {
		t.Fatalf("执行 todo_create 工具失败: %v", err)
	}

	// 验证结果
	if len(run.TodoItems) != 1 {
		t.Fatalf("预期1个任务，实际有 %d 个", len(run.TodoItems))
	}

	item := run.TodoItems[0]
	if item.Content != "LLM 生成的任务" {
		t.Errorf("任务内容错误: 期望 'LLM 生成的任务'，实际 '%s'", item.Content)
	}

	t.Logf("LLM 工具调用成功: %s", result)
}