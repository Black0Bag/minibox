# minibox 开发计划

## 已完成功能

### Phase 3.5: 性能监控 API
- [x] `internal/monitor/monitor.go` - 系统性能采集器（CPU/内存/磁盘/进程）
- [x] `internal/app/monitor_handlers.go` - REST API handlers
- [x] `internal/monitor/monitor_test.go` - 单元测试
- [x] REST endpoints: `/api/v1/monitor/metrics`, `/api/v1/monitor/history`

### B9: To-do List 长程任务
- [x] `internal/domain/agent/engine.go` - 添加 TodoItem/TodoStatus 类型
- [x] `internal/infrastructure/tools/todo.go` - todo_create/todo_update/todo_list/todo_delete 工具
- [x] `internal/infrastructure/tools/todo_test.go` - 单元测试
- [x] `internal/infrastructure/tools/todo_integration_test.go` - 集成测试
- [x] `docs/todo-feature.md` - 功能文档
- [x] 上下文注入机制（engine → tools）

## 待开发功能

- [ ] MCP 客户端集成
- [ ] 子 Agent 协作
- [ ] 角色卡系统
- [ ] 知识库 RAG 优化
- [ ] 多模态输入支持
