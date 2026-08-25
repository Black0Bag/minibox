# 项目架构（Structure）

## 目录结构

- `cmd/minibox/`：薄入口、配置、信号和生命周期。
- `internal/app/`：组合根、REST handler、会话和设备装配。
- `internal/domain/`：Agent、LLM、memory、permission、scheduler、teamwork、tools 等接口和类型。
- `internal/infrastructure/`：SQLite/FTS5/向量、LLM、编译/蒸馏、工具、调度、备份和升级。
- `internal/transport/`：HTTP、SSE、WebSocket 和统一 Envelope。
- `internal/device/`：设备 Hub、配对、审计、护栏和 Agent 工具适配。
- `internal/platform/`：错误、事件总线、重试、时间戳、文件安全和实例锁。
- `docs/`：当前有效文档与前端交接资料。

## 模块划分

- **应用层**：装配依赖，不把跨层业务散落到入口。
- **领域层**：定义业务模型和消费者接口。
- **基础设施层**：实现数据库、外部 API、工具和调度。
- **传输层**：把 HTTP/SSE/WS 协议转换为应用调用。
- **设备层**：维护设备状态、命令回执、审计和安全护栏。

## 数据流/调用关系

```text
REST/SSE/WS
    ↓
transport
    ↓
internal/app 组合根与 handler
    ↓
domain 接口
    ↓
infrastructure 实现
    ↓
SQLite / LLM / Embedding / 模拟或真实设备
```

Agent 流程：请求 → MemoryGate → FeatureRouter → LLM → 权限链 → 工具 → 结果回填 → 状态事件。

## 依赖与外部接口

- SQLite：`modernc.org/sqlite`，包含 FTS5 和向量索引使用路径。
- LLM/Embedding：OpenAI 兼容 HTTP 接口。
- WebSocket：JSON-RPC 2.0 设备通道。
- SSE：按会话的事件发布和有限历史回放。
- 调度：进程内 Cron/Alarm。

## 关键入口文件

- `cmd/minibox/main.go`
- `internal/app/app.go`
- `internal/app/http_handlers.go`
- `internal/infrastructure/llm/openai_compat.go`
- `internal/infrastructure/storage/store.go`
- `internal/transport/sse/server.go`
- `internal/transport/ws/server.go`

## 高风险模块

- 外部 LLM/Embedding：响应格式、限流、凭据和业务失败语义。
- SQLite 迁移、FTS5、向量维度：数据和索引一致性。
- Agent 工具权限：文件、Shell、外部工具和设备副作用。
- WebSocket/SSE/调度 goroutine：并发、重连、重入和资源释放。
- 备份回滚和自升级：覆盖数据库/二进制及进程生命周期。