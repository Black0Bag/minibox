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
	run := &agent.Run{
		ID: "test-run-1", SessionID: "test-session-1",
		State: agent.StatePlanning, Mode: agent.ModeBuild,
		TodoItems: []agent.TodoItem{},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	createTool := NewTodoCreate()
	updateTool := NewTodoUpdate()
	listTool := NewTodoList()
	deleteTool := NewTodoDelete()

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
		if run.TodoItems[0].Content != "测试任务1" {
			t.Errorf("任务内容错误")
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
		if run.TodoItems[0].Status != agent.TodoInProgress {
			t.Errorf("任务状态错误")
		}
		t.Logf("更新任务状态成功: %s", result)
	})

	// 测试列出任务
	t.Run("ListTasks", func(t *testing.T) {
		result, err := listTool.Invoke(ctx, []byte(`{}`))
		if err != nil {
			t.Fatalf("列出任务失败: %v", err)
		}
		t.Logf("列出任务成功:\n%s", result)
	})

	// 测试创建更多任务
	t.Run("CreateMoreTasks", func(t *testing.T) {
		createTool.Invoke(ctx, []byte(`{"content":"测试任务2"}`))
		createTool.Invoke(ctx, []byte(`{"content":"测试任务3"}`))
		if len(run.TodoItems) != 3 {
			t.Fatalf("预期3个任务，实际有 %d 个", len(run.TodoItems))
		}
	})

	// 测试标记任务完成
	t.Run("CompleteTask", func(t *testing.T) {
		item := run.TodoItems[0]
		input := []byte(`{"id":"` + item.ID + `","status":"completed"}`)
		_, err := updateTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("标记任务完成失败: %v", err)
		}
		if run.TodoItems[0].Status != agent.TodoCompleted {
			t.Errorf("任务状态错误")
		}
	})

	// 测试删除任务
	t.Run("DeleteTask", func(t *testing.T) {
		item := run.TodoItems[1]
		input := []byte(`{"id":"` + item.ID + "",\"content\":\"测试任务2\""}")
		_, err := deleteTool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("删除任务失败: %v", err)
		}
		if len(run.TodoItems) != 2 {
			t.Fatalf("预期2个任务，实际有 %d 个", len(run.TodoItems))
		}
	})

	// 测试最终状态
	t.Run("FinalState", func(t *testing.T) {
		result, _ := listTool.Invoke(ctx, []byte(`{}`))
		t.Logf("最终任务状态:\n%s", result)
	})
}

// TestTodoManagerIntegration 测试 TodoManager 集成。
func TestTodoManagerIntegration(t *testing.T) {
	run := &agent.Run{
		TodoItems: []agent.TodoItem{
			{ID: "task-1", Content: "任务1", Status: agent.TodoPending, CreatedAt: time.Now()},
			{ID: "task-2", Content: "任务2", Status: agent.TodoPending, CreatedAt: time.Now()},
			{ID: "task-3", Content: "任务3", Status: agent.TodoPending, CreatedAt: time.Now()},
		},
	}

	manager := NewTodoManager(run)

	// 测试获取下一个待处理任务
	next := manager.GetNextPending()
	if next == nil || next.ID != "task-1" {
		t.Error("GetNextPending 错误")
	}

	// 标记 task-1 为进行中
	manager.MarkInProgress("task-1")
	if run.TodoItems[0].Status != agent.TodoInProgress {
		t.Error("MarkInProgress 错误")
	}

	// 标记 task-1 为已完成
	manager.MarkCompleted("task-1")
	if run.TodoItems[0].Status != agent.TodoCompleted {
		t.Error("MarkCompleted 错误")
	}

	// 下一个待处理任务应为 task-2
	next = manager.GetNextPending()
	if next == nil || next.ID != "task-2" {
		t.Error("GetNextPendingAfterCompletion 错误")
	}

	// 标记 task-2 为进行中
	manager.MarkInProgress("task-2")
	current := manager.GetCurrentInProgress()
	if current == nil || current.ID != "task-2" {
		t.Error("GetCurrentInProgress 错误")
	}

	// 进度统计
	completed, total := manager.GetProgress()
	if total != 3 || completed != 1 {
		t.Errorf("GetProgress 错误: completed=%d total=%d", completed, total)
	}

	// 获取所有任务
	all := manager.GetAll()
	if len(all) != 3 {
		t.Errorf("GetAll 错误: len=%d", len(all))
	}
}

// TestTodoWithEngineIntegration 测试与 Agent 引擎的集成。
func TestTodoWithEngineIntegration(t *testing.T) {
	run := &agent.Run{
		ID: "test-run-3", SessionID: "test-session-3",
		State: agent.StatePlanning, Mode: agent.ModeBuild,
		TodoItems: []agent.TodoItem{},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, runContextKey{}, run)

	// 模拟 LLM 返回 todo_create 工具调用
	todoCall := llm.ToolCall{
		ID: "call-1", Name: "todo_create",
		Arguments: `{"content":"LLM 生成的任务"}`,
	}

	createTool := NewTodoCreate()
	result, err := createTool.Invoke(ctx, []byte(todoCall.Arguments))
	if err != nil {
		t.Fatalf("执行 todo_create 工具失败: %v", err)
	}

	if len(run.TodoItems) != 1 {
		t.Fatalf("预期1个任务，实际有 %d 个", len(run.TodoItems))
	}

	if run.TodoItems[0].Content != "LLM 生成的任务" {
		t.Errorf("任务内容错误")
	}

	t.Logf("LLM 工具调用成功: %s", result)
}