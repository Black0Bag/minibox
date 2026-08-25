# 编码规范（Rules）

## 默认规则（创建即生效）

- 后端保持 domain / infrastructure / transport / app 组合根分层。
- 配置集中管理；真实密钥、令牌、设备凭据和私有 URL 不得进入源码、文档、日志、测试报告或记忆。
- 外部 API 同时检查状态码、Content-Type、响应结构和业务字段；HTTP 200 不等于业务成功。
- 关键流程返回可定位错误；异步作业暴露明确成功/失败状态。
- 数据库迁移先在独立测试库验证，不直接覆盖稳定数据。

## 命名规范

- Go 导出 DTO 使用稳定 lower_snake_case JSON tag。
- 测试与服务实例使用能表明隔离范围的名称。

## 代码风格

- 采用 Go 标准格式（gofmt）；优先小函数和清晰错误上下文。
- 测试优先 table-driven 和 fixture 模式。

## 错误处理

- 边界错误包装上下文并返回 typed error；不 panic 正常流程。
- 外部响应必须同时检查 HTTP 状态、Content-Type 和业务字段。

## 日志与注释

- 日志记录关联 ID、状态和错误类别，不记录密钥或完整请求体。
- 注释解释兼容策略和为什么做，不重复代码行为。

## API 与前端契约

- REST 字段使用稳定的 lower_snake_case JSON tag。
- 成功响应使用统一 Envelope；错误响应遵循 RFC 7807 风格。
- SSE 按 session 使用单一订阅、seq 去重和 Last-Event-ID 重连。
- WebSocket 使用 JSON-RPC 2.0；请求 ID 必须唯一，响应按 ID 归属。
- 前端不得直接访问 SQLite 或依赖后端 internal 包。

## 安全与隐私

- 设备写操作、外部工具获取、自升级和数据库恢复保持 fail-closed/审批边界。
- 测试使用 Mock、模拟设备和独立数据库；真实副作用必须单独授权。
- 日志只能记录非敏感关联 ID、状态和错误类别。

## 测试与回归要求

- 每项修复覆盖主流程、失败分支和历史回归。
- 提交前运行 `gofmt`、`go test -count=1 ./...`、`go vet ./...` 和构建。
- 报告区分 Mock、模拟设备、真实外部服务和未测项。
- 测试源文件暂时保留，并由 `docs/test-inventory.md` 维护名录；稳定期再按名录精简。

## 禁止事项（反模式）

- 禁止用 HTTP 200 替代业务验收。
- 禁止用 LIKE 降级掩盖 FTS5 或向量检索失败。
- 禁止前端直连后端 SQLite。