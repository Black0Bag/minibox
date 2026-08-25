# 最终验收摘要

## 当前结论

后端核心修复和最终冒烟阶段已完成，当前进入仓库收尾与 Android 前端交接阶段。

## 已验证

- 全仓 `go test -count=1 ./...` 通过。
- `go vet ./...` 通过。
- 后端最新源码构建成功。
- REST 健康、就绪、状态、工具、权限、LLM、知识库、设备、调度、监控和会话接口已完成隔离实例冒烟。
- SSE 返回 `200 text/event-stream`，支持序号和 `Last-Event-ID` 回放。
- WebSocket 单读循环、JSON-RPC pending map、并发乱序回执已完成测试。
- 知识编译结构化提炼、FTS5 中文索引、Embedding 契约和混合检索已完成回归。
- 18 个设备工具已注册；写操作需要审批，Plan 模式 fail-closed。
- **REST Bearer Token 认证已实现**：无 token/错误 token 返回 401，正确 token 返回 200，`/health`、`/ready`、`/device/ws` 白名单放行。Token 自动生成并存储在 `data/auth.token`（0600）。
- **Agent 审批 API 已实现**：`POST /api/v1/approvals/{run_id}`，Agent 遇到需审批工具时暂停等待外部提交，批准后恢复执行，拒绝后回退 Planning。
- **会话持久化已实现**：用户消息和助手回答写入 `conversation_log` 表，启动时恢复最近 50 条会话。

## 真实外部服务限制

真实 LLM 连续请求曾受到供应商 RPM 限流。供应商 `429` 不作为源码缺陷判定；真实服务结果必须和 Mock/本地回归结果分开记录。

## 未取得的证据

- 当前 Android/arm64 环境无法运行 ThreadSanitizer，因此没有本机 `go test -race ./...` 证据。
- 未操作真实 Android 设备的屏幕、输入、通知等副作用。
- 未执行自升级应用、主数据库恢复和外部平台写操作。

## 证据来源

- 源码测试：`internal/**/**/*_test.go`
- 构建和静态检查：工作区后端仓库的 Go 命令输出
- 运行冒烟：稳定隔离验证实例的健康、就绪、REST、SSE 和日志结果
- 具体协议：[`api.md`](api.md)、[`sse.md`](sse.md)、[`websocket.md`](websocket.md)

## 使用原则

当前文档只描述已复核的最终状态。若历史文档与本文件冲突，以当前源码、测试输出和本文件为准。