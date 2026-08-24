# To-do List 长程任务功能（B9）

## 概述

To-do List 长程任务功能允许 Agent 在运行过程中创建、管理和跟踪待办任务列表。该功能基于 Claude Code V2 的 `TodoWrite` 工具设计，支持任务状态的流转和中断续跑。

## 核心特性

1. **任务状态管理**：支持 `pending`、`in_progress`、`completed` 三种状态
2. **任务生命周期**：创建 → 进行中 → 完成（或删除）
3. **持久化支持**：TodoItems 作为 Run 的一部分持久化，支持崩溃续跑
4. **工具接口**：提供四个标准工具供 LLM 调用

## 架构设计

### Domain 层（类型定义）

在 `internal/domain/agent/engine.go` 中定义：

```go
// TodoStatus 任务项状态
type TodoStatus string

const (
    TodoPending    TodoStatus = "pending"    // 等待中
    TodoInProgress TodoStatus = "in_progress" // 执行中
    TodoCompleted  TodoStatus = "completed"   // 已完成
)

// TodoItem to-do 任务项
type TodoItem struct {
    ID          string    `json:"id"`
    Content     string    `json:"content"`
    Status      TodoStatus `json:"status"`
    CreatedAt   time.Time `json:"created_at"`
    StartedAt   time.Time `json:"started_at,omitempty"`
    CompletedAt time.Time `json:"completed_at,omitempty"`
}
```

在 `Run` 结构体中添加了 `TodoItems` 字段：

```go
type Run struct {
    // ... 其他字段
    TodoItems []TodoItem `json:"todo_items,omitempty"`
    // ...
}
```

### Infrastructure 层（工具实现）

在 `internal/infrastructure/tools/todo.go` 中实现：

1. **todo_create**：创建新的待办任务项
2. **todo_update**：更新任务状态（pending → in_progress → completed）
3. **todo_list**：列出所有待办任务项
4. **todo_delete**：删除待办任务项

### 上下文注入

在 `internal/infrastructure/engine/engine.go` 中，当执行工具时将 Run 注入到上下文中：

```go
// stepActing 执行状态：执行一个工具。
func (e *Engine) stepActing(ctx context.Context, run *agent.Run) (*agent.Run, error) {
    // ... 
    if e.tools != nil {
        // 将 Run 注入到上下文中，供 todo 工具使用
        ctx = context.WithValue(ctx, runContextKey{}, run)
        out, err := e.tools.Execute(ctx, *call)
        // ...
    }
    // ...
}
```

### 工具注册

在 `internal/app/app.go` 的 `buildTools` 方法中注册 todo 工具：

```go
// B9 待办任务工具（to-do list 长程任务管理）
for _, t := range []tools.Tool{
    infratools.NewTodoCreate(),
    infratools.NewTodoUpdate(),
    infratools.NewTodoList(),
    infratools.NewTodoDelete(),
} {
    if err := reg.Register(t); err != nil {
        return err
    }
}
```

## 使用示例

### LLM 调用示例

1. **创建任务**：
```json
{
    "name": "todo_create",
    "arguments": {
        "content": "分析用户需求并设计解决方案"
    }
}
```

2. **更新任务状态**：
```json
{
    "name": "todo_update",
    "arguments": {
        "id": "任务ID",
        "status": "in_progress"
    }
}
```

3. **列出所有任务**：
```json
{
    "name": "todo_list",
    "arguments": {}
}
```

4. **删除任务**：
```json
{
    "name": "todo_delete",
    "arguments": {
        "id": "任务ID"
    }
}
```

## 辅助功能

### TodoManager

提供了一个 `TodoManager` 辅助结构，用于管理任务状态：

```go
// 创建管理器
manager := NewTodoManager(run)

// 获取下一个待处理任务
nextPending := manager.GetNextPending()

// 获取当前正在执行的任务
currentInProgress := manager.GetCurrentInProgress()

// 标记任务为进行中
err := manager.MarkInProgress(taskID)

// 标记任务为已完成
err := manager.MarkCompleted(taskID)

// 获取进度统计
completed, total := manager.GetProgress()
```

## 测试

### 单元测试

- `todo_test.go`：测试单个工具的功能
- `todo_integration_test.go`：测试工具集成和 TodoManager 功能

### 测试覆盖

1. 创建任务并验证状态
2. 更新任务状态（pending → in_progress → completed）
3. 列出任务并验证内容
4. 删除任务并验证移除
5. TodoManager 辅助功能测试
6. 与 Agent 引擎的集成测试

## 设计原则

1. **遵循 Claude Code V2 设计**：参考 `TodoWrite` 工具结构
2. **状态机模式**：任务状态严格遵循 pending → in_progress → completed 流转
3. **持久化支持**：TodoItems 作为 Run 的一部分，支持崩溃续跑
4. **工具标准化**：提供标准的 CRUD 操作接口
5. **上下文注入**：通过 Go context 传递 Run，避免全局状态

## 未来扩展

1. **任务依赖**：支持任务间的依赖关系
2. **任务优先级**：添加任务优先级字段
3. **任务截止时间**：支持设置任务截止时间
4. **任务分组**：支持将任务分组管理
5. **任务进度百分比**：支持子任务和进度跟踪

## 架构约束

1. **遵循架构守护**：工具实现符合 infrastructure 层规范
2. **依赖注入**：通过 Go context 传递依赖，避免循环依赖
3. **只读/写分离**：`todo_list` 为只读工具，其他为写工具
4. **测试覆盖**：提供完整的单元测试和集成测试