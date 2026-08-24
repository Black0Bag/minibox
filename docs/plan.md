# minibox 后端补齐计划

## 任务范围
补齐 6 大缺失功能，彻底落地后端路线图

## 实施顺序（按依赖关系）

### 1. 模型参数全面（TopP/FrequencyPenalty/PresencePenalty/StopSequences）
- **改动文件**：
  - `internal/domain/llm/types.go` — `Request` 结构体追加字段
  - `internal/infrastructure/llm/openai_compat.go` — `chatCompletionReq` 追加字段 + `buildRequest()` 传递
  - `internal/infrastructure/engine/engine.go` — `stepPlanning()` 调用时传递参数
- **验证**：`go build ./...` + `go test ./...` + `go vet ./...`

### 2. 思考强度多供应商适配（Anthropic extended thinking）
- **改动文件**：
  - `internal/infrastructure/llm/openai_compat.go` — `buildRequest()` 扩展 Anthropic `thinking` 参数
- **验证**：同上

### 3. 性能监控 API
- **新增文件**：`internal/monitor/monitor.go` — CPU/内存/磁盘/进程采集
- **改动文件**：
  - `internal/app/http_handlers.go` — 新增 `handleMonitor...` 端点
  - `internal/app/app.go` — 挂载监控端点
- **验证**：同上

### 4. To-do list 长程任务
- **改动文件**：
  - `internal/domain/agent/engine.go` — `Run` 结构体追加 `TodoItems` 字段
  - `internal/infrastructure/engine/engine.go` — 扩展运行状态机
  - `internal/app/http_handlers.go` — 新增 todo 端点
  - `internal/app/app.go` — 挂载 todo 端点
- **验证**：同上

### 5. CLI 配置模式（--init）
- **改动文件**：`cmd/minibox/main.go` — 新增 `--init` flag
- **验证**：`go build ./...`

### 6. API 中文文档
- **新增文件**：`docs/api.md` — OpenAPI 3.0 中文文档
- **验证**：文件完整性检查