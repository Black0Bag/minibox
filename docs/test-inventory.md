# Go 测试源文件名录

> 本名录对应当前保留的 Go 测试源文件。测试源文件暂不删除；项目稳定后可按本名录和覆盖价值进行精简。带有真实外部服务依赖的测试通过环境变量或独立 fixture 控制，不应默认发起外部请求。

## App / 组合根

- `internal/app/http_handlers_test.go`：REST handler 与参数/错误分支。
- `internal/app/integration_test.go`：可选真实 LLM 集成测试入口；需要显式环境变量。
- `internal/app/prefix_distill_test.go`：知识提炼结构化 JSON、校验和失败回退。
- `internal/app/device_tool_integration_test.go`：设备工具注册、模拟回执和审批属性。
- `internal/app/device_ws_integration_test.go`：应用级 WebSocket 设备命令往返。
- `internal/app/device_ws_concurrency_test.go`：并发设备命令和乱序响应归属。
- `internal/app/device_ws_test_helpers_test.go`：设备 WS 集成测试辅助代码。

## 架构与配置

- `internal/arch/arch_test.go`：包依赖和模块边界守护。
- `internal/config/config_test.go`：默认配置、YAML 覆盖和配置解析。

## 设备与权限

- `internal/device/guardrails/guard_test.go`：设备危险动作护栏。
- `internal/device/hub_test.go`：设备 Hub、连接、命令、离线和急停。
- `internal/device/tool_adapter_test.go`：设备能力到 Agent 工具的适配。
- `internal/device/unit_test.go`：设备模型、配对和审计日志。

## Domain

- `internal/domain/charactercard/book_test.go`：角色书匹配、排序和预算。
- `internal/domain/charactercard/card_test.go`：角色卡格式解析。
- `internal/domain/llm/types_test.go`：LLM 类型和请求校验。
- `internal/domain/permission/policy_test.go`：权限策略链。
- `internal/domain/setup/setup_test.go`：向导、凭据和路径护栏。
- `internal/domain/skill/skill_test.go`：Skill 读取和路径安全。
- `internal/domain/teamwork/teamwork_test.go`：团队、角色、HR 和讨论模型。
- `internal/domain/tools/tool_test.go`：工具注册、调用和 panic 防护。
- `internal/domain/worldbook/worldbook_test.go`：世界书加载和扩展。

## Agent / 调度 / 团队

- `internal/infrastructure/engine/engine_test.go`：Agent 状态机、预算和持久化。
- `internal/infrastructure/engine/memory_gate_test.go`：记忆门检索注入。
- `internal/infrastructure/engine/orchestrator_test.go`：子 Agent 编排。
- `internal/infrastructure/scheduler/cron_test.go`：Cron、Alarm、预算和重叠执行。
- `internal/infrastructure/teamwork/coordinator_test.go`：团队协作编排。

## LLM 与知识库

- `internal/infrastructure/llm/embedding_test.go`：Embedding URL、字段、query/passage 和维度。
- `internal/infrastructure/llm/feature_router_test.go`：功能级模型路由。
- `internal/infrastructure/llm/openai_compat_test.go`：OpenAI 兼容 JSON/SSE 和异常响应。
- `internal/infrastructure/llm/router_test.go`：供应商级联、重试和熔断。
- `internal/infrastructure/storage/memory_test.go`：知识库 CRUD、FTS、编译和检索。
- `internal/infrastructure/storage/run_store_test.go`：Agent 运行持久化。
- `internal/infrastructure/storage/storage_test.go`：SQLite 迁移和 schema。
- `internal/infrastructure/storage/vector_test.go`：向量检索和混合排序。

## 工具、平台与传输

- `internal/infrastructure/tools/acquire_test.go`：外部工具获取和校验。
- `internal/infrastructure/tools/exec_tool_test.go`：隔离执行工具。
- `internal/infrastructure/tools/filesystem_test.go`：文件工具和路径沙箱。
- `internal/infrastructure/tools/kbsearch_test.go`：知识搜索工具。
- `internal/infrastructure/tools/mcp_test.go`：MCP JSON-RPC 子进程适配器。
- `internal/infrastructure/tools/shell_test.go`：Shell 工具安全边界。
- `internal/infrastructure/tools/todo_test.go`：Todo 工具状态机。
- `internal/infrastructure/tools/todo_integration_test.go`：Todo 集成。
- `internal/infrastructure/upgrade/upgrade_test.go`：升级校验和回滚逻辑。
- `internal/monitor/monitor_test.go`：系统和进程指标采集。
- `internal/platform/errors/errors_test.go`：RFC 7807 错误。
- `internal/platform/eventbus/eventbus_test.go`：事件订阅和 panic 隔离。
- `internal/platform/fsutil/logme_test.go`：审计足迹。
- `internal/platform/instance/instance_test.go`：单实例锁。
- `internal/platform/logging/logging_test.go`：日志配置。
- `internal/platform/retry/retry_test.go`：重试和取消。
- `internal/platform/timestamp/timestamp_test.go`：时间戳和序号。
- `internal/transport/envelope_test.go`：统一 Envelope。
- `internal/transport/http/server_test.go`：HTTP transport。
- `internal/transport/sse/server_test.go`：SSE 发布、序号和心跳。
- `internal/transport/sse/server_http_reconnect_test.go`：SSE HTTP 重连回放。
- `internal/transport/ws/handlers_test.go`：WS 方法和默认 handler。
- `internal/transport/ws/server_test.go`：WS 握手、路由、通知和广播。

## 精简建议

稳定后按以下顺序评估：

1. 删除只覆盖已移除实现的测试；
2. 合并重复的 fixture/helper；
3. 将真实服务测试移入显式 `integration` build tag；
4. 保留每个安全边界、协议边界和历史回归至少一条测试；
5. 每次精简后重新运行全量测试和架构守护。