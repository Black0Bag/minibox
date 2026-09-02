# 最终验收摘要

## 当前结论

后端核心修复和最终冒烟阶段已完成，当前进入仓库收尾与 Android 前端交接阶段。

2026-09-02 完成一轮契约与并发加固（11 项修复 + 3 个深挖缺陷），
CI 四个 job（测试 / 竞态检测 / 静态检查 / 依赖审计）**首次全绿**。

## 已验证

- 全仓 `go test -count=1 ./...` 通过（34 包）。
- `go vet ./...` 通过。
- `gofmt -l internal cmd` 无输出。
- `golangci-lint run`（v2.12.2，与 CI 同版本）**0 issues**。
- `go test -race ./...` 通过（GitHub Actions Linux x86_64 job；本机 arm64 不支持）。
- 后端最新源码构建成功。
- REST 健康、就绪、状态、工具、权限、LLM、知识库、设备、调度、监控和会话接口已完成隔离实例冒烟。
- SSE 返回 `200 text/event-stream`，支持序号和 `Last-Event-ID` 回放。
- WebSocket 单读循环、JSON-RPC pending map、并发乱序回执已完成测试。
- 知识编译结构化提炼、FTS5 中文索引、Embedding 契约和混合检索已完成回归。
- 18 个设备工具已注册；写操作需要审批，Plan 模式 fail-closed。
- **REST Bearer Token 认证已实现**：无 token/错误 token 返回 401，正确 token 返回 200，`/health`、`/ready`、`/device/ws` 白名单放行。Token 自动生成并存储在 `data/auth.token`（0600）。
- **Agent 审批 API 已实现**：`POST /api/v1/approvals/{run_id}`，Agent 遇到需审批工具时暂停等待外部提交，批准后恢复执行，拒绝后回退 Planning。
- **会话持久化已实现**：用户消息和助手回答写入 `conversation_log` 表，启动时恢复最近 50 条会话。

## 2026-09-02 契约与并发加固

### 错误响应契约统一

- 401 补齐 `WWW-Authenticate` 挑战头（RFC 9110 §15.5.2）；凭据无效时带
  `error="invalid_token"`（RFC 6750 §3）；scheme 大小写不敏感（RFC 9110 §11.1）。
- 401 / 404 / 405 全部改为 `application/problem+json`，与业务错误一致。
  修复前 401 是 `text/plain` + 自造 `{"error":...}`，404/405 被包进成功 Envelope。
- Token 比较改用 `crypto/subtle.ConstantTimeCompare`。
- 认证中间件收敛为 `internal/transport/http/auth.go` 唯一实现（原先两份并存，
  未接线的那份还缺 `/device/ws` 白名单）。
- 实测证据：无 token → 401 + `realm="minibox"`；错 token → 追加
  `error="invalid_token"`；小写 `bearer` → 200；404/405 → problem+json。

### DTO 契约冻结

- teamwork 域 7 个结构体 30 个字段补齐 `json` tag，统一 lower_snake_case。
  修复前 `Project`/`Discussion`/`Triage` 输出大驼峰，与内层 `Team` 的
  snake_case 混用。
- 新增 10 个契约测试锁定字段名，含「任何大驼峰字段名即失败」的兜底用例。
- `discussion.proposals` 保留整数键 map（JSON 为字符串键对象），
  `timeout_ns` 显式标注纳秒单位。

### 并发安全

- `historyRing`（/monitor/history）加锁：30s 采样 goroutine 与 HTTP handler
  并发访问同一切片。
- 会话 hub 全面持锁 + 对外交出快照副本：`driveRun` goroutine 与 HTTP
  序列化并发。
- `transport/http.Server.httpSrv` 加 `srvMu`：`Start` 在 goroutine 写、
  `Shutdown` 与测试从别处读（CI race job 报出 4 处）。
- `ws.Client` 元数据（id/name/handshake/data）改访问器 + `metaMu` 保护。
- 新增 3 组并发测试（历史缓冲 8 写 8 读、会话 4 写 4 读、WS 通知 channel 交接）。

### 深挖出的三个真实缺陷（原计划外）

1. `driveRun` 循环内 `run, err := h.agent.Step(...)` 用 `:=` 新建变量，
   外层循环条件始终读初始状态，**状态机实际从未推进**。
2. `Send` 在会话不存在时调 `Create()` 生成随机 ID，用户消息写进另一个会话，
   前端按自己传的 ID 拉不到消息。
3. `finishRun` 未清理 `pendingRuns`，正常结束的 run 永久占用审批入口。

### 其他

- `session.List()` 实现按 `updated_at` 倒序（原注释声称倒序但直接遍历 map）。
- `RestoreSessions` 改用 `conversation_log.created_at` 真实时间，
  不再把历史会话标成「刚刚」；消息 `at` 统一 RFC3339。
- `persistMessage` 持久化失败改为 `slog.Warn` 留痕（原条件判断本身有误）。
- `golangci-lint` 36 项告警清零；`.golangci.yml` 为测试文件的
  `defer Close()` 与固定签名参数加了合理豁免。
- 删除测试残留 `internal/app/data/auth.token`；`.gitignore` 补 `/minibox` 与
  codebase 扫描产物。

## 真实外部服务限制

真实 LLM 连续请求曾受到供应商 RPM 限流。供应商 `429` 不作为源码缺陷判定；真实服务结果必须和 Mock/本地回归结果分开记录。

## 未取得的证据

- 本机 Android/arm64 环境无法运行 ThreadSanitizer，`-race` 证据由
  GitHub Actions 的 Linux x86_64 job 提供（已通过）。
- 未操作真实 Android 设备的屏幕、输入、通知等副作用。
- 未执行自升级应用、主数据库恢复和外部平台写操作。
- 浏览器代理（`browser.*`）仅有 WS 方法常量与占位 handler，Agent 侧无对应
  工具、Hub 无下发路径，尚不能实际调用。

## 证据来源

- 源码测试：`internal/**/**/*_test.go`
- 构建和静态检查：工作区后端仓库的 Go 命令输出 + `golangci-lint` 本机运行
- 竞态检测：GitHub Actions CI 竞态检测 job
- 运行冒烟：稳定隔离验证实例的健康、就绪、REST、SSE 和日志结果
- 具体协议：[`api.md`](api.md)、[`sse.md`](sse.md)、[`websocket.md`](websocket.md)

## 使用原则

当前文档只描述已复核的最终状态。若历史文档与本文件冲突，以当前源码、测试输出和本文件为准。