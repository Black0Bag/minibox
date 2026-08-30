// Package tools 内置工具实现（infrastructure 层）。
// 设计：
//   - 复用 platform/fsutil.PathValidator 做路径沙箱（防越权，golang-security）
//   - exec 一律独立参数传参，绝不拼 shell（禁 bash -c，golang-security）
//   - 工具只读/只写/破坏性元数据标注，供权限门控用
//   - 全部经 domain/tools.SafeInvoke 调用（panic 恢复 + 输出上限）
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/agent"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	"github.com/google/uuid"
)

// todoTool todo 工具公共载体。
type todoTool struct {
	name   string
	desc   string
	schema json.RawMessage
	meta   tools.Metadata
	fn     func(ctx context.Context, run *agent.Run, args map[string]json.RawMessage) (string, error)
}

func (t *todoTool) Name() string                { return t.name }
func (t *todoTool) Description() string         { return t.desc }
func (t *todoTool) JSONSchema() json.RawMessage { return t.schema }
func (t *todoTool) Metadata() tools.Metadata    { return t.meta }

func (t *todoTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var args map[string]json.RawMessage
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
	}
	if args == nil {
		args = make(map[string]json.RawMessage)
	}

	// 从上下文获取当前 Run（需要注入）
	run := getRunFromContext(ctx)
	if run == nil {
		return "", fmt.Errorf("无法获取当前 Agent Run")
	}

	return t.fn(ctx, run, args)
}

// getRunFromContext 从上下文中获取 Agent Run。
func getRunFromContext(ctx context.Context) *agent.Run {
	// 从上下文中获取 Run
	if run, ok := ctx.Value(runContextKey{}).(*agent.Run); ok {
		return run
	}
	return nil
}

// runContextKey 上下文键，用于存储 Agent Run（与 engine 包保持一致）。
type runContextKey struct{}

// NewTodoCreate 创建 todo 任务项。
func NewTodoCreate() tools.Tool {
	return &todoTool{
		name: "todo_create",
		desc: "创建新的待办任务项。输入 {content}。返回创建的任务ID。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"content":{"type":"string","description":"任务描述内容"}
			},
			"required":["content"]
		}`),
		meta: tools.Metadata{
			ReadOnly:         false,
			ConcurrencySafe:  true,
			MaxResultSize:    512,
			RiskTier:         "low",
			RequiresApproval: false,
		},
		fn: func(ctx context.Context, run *agent.Run, args map[string]json.RawMessage) (string, error) {
			content, err := strArg(args, "content")
			if err != nil {
				return "", err
			}

			// 生成新任务ID
			id := uuid.New().String()

			// 创建新的 TodoItem
			item := agent.TodoItem{
				ID:        id,
				Content:   content,
				Status:    agent.TodoPending,
				CreatedAt: time.Now(),
			}

			// 添加到 Run 的 TodoItems 列表
			run.TodoItems = append(run.TodoItems, item)

			return fmt.Sprintf("已创建任务: %s (ID: %s)", content, id), nil
		},
	}
}

// NewTodoUpdate 更新 todo 任务状态。
func NewTodoUpdate() tools.Tool {
	return &todoTool{
		name: "todo_update",
		desc: "更新待办任务状态。输入 {id, status}。status 可选: pending, in_progress, completed。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"id":{"type":"string","description":"任务ID"},
				"status":{"type":"string","enum":["pending","in_progress","completed"],"description":"新状态"}
			},
			"required":["id","status"]
		}`),
		meta: tools.Metadata{
			ReadOnly:         false,
			ConcurrencySafe:  true,
			MaxResultSize:    512,
			RiskTier:         "low",
			RequiresApproval: false,
		},
		fn: func(ctx context.Context, run *agent.Run, args map[string]json.RawMessage) (string, error) {
			id, err := strArg(args, "id")
			if err != nil {
				return "", err
			}

			statusStr, err := strArg(args, "status")
			if err != nil {
				return "", err
			}

			// 解析状态
			var status agent.TodoStatus
			switch statusStr {
			case "pending":
				status = agent.TodoPending
			case "in_progress":
				status = agent.TodoInProgress
			case "completed":
				status = agent.TodoCompleted
			default:
				return "", fmt.Errorf("无效的状态: %s", statusStr)
			}

			// 查找并更新任务
			for i := range run.TodoItems {
				if run.TodoItems[i].ID == id {
					oldStatus := run.TodoItems[i].Status
					run.TodoItems[i].Status = status

					// 更新时间戳
					now := time.Now()
					switch status {
					case agent.TodoInProgress:
						run.TodoItems[i].StartedAt = now
					case agent.TodoCompleted:
						run.TodoItems[i].CompletedAt = now
					}

					return fmt.Sprintf("任务 %s 状态已从 %s 更新为 %s", id, oldStatus, status), nil
				}
			}

			return "", fmt.Errorf("未找到ID为 %s 的任务", id)
		},
	}
}

// NewTodoList 列出所有 todo 任务项。
func NewTodoList() tools.Tool {
	return &todoTool{
		name: "todo_list",
		desc: "列出所有待办任务项。无输入参数。返回所有任务的ID、内容和状态。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{}
		}`),
		meta: tools.Metadata{
			ReadOnly:         true,
			ConcurrencySafe:  true,
			MaxResultSize:    4096,
			RiskTier:         "low",
			RequiresApproval: false,
		},
		fn: func(ctx context.Context, run *agent.Run, args map[string]json.RawMessage) (string, error) {
			if len(run.TodoItems) == 0 {
				return "当前没有待办任务", nil
			}

			var sb strings.Builder
			sb.WriteString("待办任务列表:\n")

			for _, item := range run.TodoItems {
				statusEmoji := "⏳"
				switch item.Status {
				case agent.TodoInProgress:
					statusEmoji = "🔄"
				case agent.TodoCompleted:
					statusEmoji = "✅"
				}

				sb.WriteString(fmt.Sprintf("%s [%s] %s\n", statusEmoji, item.ID[:8], item.Content))
			}

			return sb.String(), nil
		},
	}
}

// NewTodoDelete 删除 todo 任务项。
func NewTodoDelete() tools.Tool {
	return &todoTool{
		name: "todo_delete",
		desc: "删除待办任务项。输入 {id}。返回删除结果。",
		schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"id":{"type":"string","description":"要删除的任务ID"}
			},
			"required":["id"]
		}`),
		meta: tools.Metadata{
			ReadOnly:         false,
			ConcurrencySafe:  true,
			MaxResultSize:    512,
			RiskTier:         "low",
			RequiresApproval: false,
		},
		fn: func(ctx context.Context, run *agent.Run, args map[string]json.RawMessage) (string, error) {
			id, err := strArg(args, "id")
			if err != nil {
				return "", err
			}

			// 查找并删除任务
			for i := range run.TodoItems {
				if run.TodoItems[i].ID == id {
					deletedItem := run.TodoItems[i]
					// 从切片中删除
					run.TodoItems = append(run.TodoItems[:i], run.TodoItems[i+1:]...)

					return fmt.Sprintf("已删除任务: %s (ID: %s)", deletedItem.Content, id), nil
				}
			}

			return "", fmt.Errorf("未找到ID为 %s 的任务", id)
		},
	}
}

// TodoManager 管理 todo 任务的辅助结构。
type TodoManager struct {
	run *agent.Run
}

// NewTodoManager 创建新的 TodoManager。
func NewTodoManager(run *agent.Run) *TodoManager {
	return &TodoManager{run: run}
}

// GetNextPending 获取下一个待处理任务。
func (m *TodoManager) GetNextPending() *agent.TodoItem {
	for i := range m.run.TodoItems {
		if m.run.TodoItems[i].Status == agent.TodoPending {
			return &m.run.TodoItems[i]
		}
	}
	return nil
}

// GetCurrentInProgress 获取当前正在执行的任务。
func (m *TodoManager) GetCurrentInProgress() *agent.TodoItem {
	for i := range m.run.TodoItems {
		if m.run.TodoItems[i].Status == agent.TodoInProgress {
			return &m.run.TodoItems[i]
		}
	}
	return nil
}

// MarkInProgress 标记任务为进行中。
func (m *TodoManager) MarkInProgress(id string) error {
	for i := range m.run.TodoItems {
		if m.run.TodoItems[i].ID == id {
			if m.run.TodoItems[i].Status != agent.TodoPending {
				return fmt.Errorf("只能将 pending 状态的任务标记为 in_progress")
			}
			m.run.TodoItems[i].Status = agent.TodoInProgress
			m.run.TodoItems[i].StartedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("未找到ID为 %s 的任务", id)
}

// MarkCompleted 标记任务为已完成。
func (m *TodoManager) MarkCompleted(id string) error {
	for i := range m.run.TodoItems {
		if m.run.TodoItems[i].ID == id {
			if m.run.TodoItems[i].Status != agent.TodoInProgress {
				return fmt.Errorf("只能将 in_progress 状态的任务标记为 completed")
			}
			m.run.TodoItems[i].Status = agent.TodoCompleted
			m.run.TodoItems[i].CompletedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("未找到ID为 %s 的任务", id)
}

// GetAll 获取所有任务。
func (m *TodoManager) GetAll() []agent.TodoItem {
	return m.run.TodoItems
}

// GetProgress 获取任务进度统计。
func (m *TodoManager) GetProgress() (completed, total int) {
	total = len(m.run.TodoItems)
	for _, item := range m.run.TodoItems {
		if item.Status == agent.TodoCompleted {
			completed++
		}
	}
	return completed, total
}
