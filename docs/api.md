# minibox REST API

> 默认基础地址：`http://127.0.0.1:8086`
> 路径前缀：`/api/v1`
> 字符编码：UTF-8

本文是前端实现索引，字段以当前 Go DTO 和 handler 为准。SSE 与 WebSocket 分别见 [`sse.md`](sse.md) 和 [`websocket.md`](websocket.md)。

## 通用响应

### 成功 Envelope

```json
{
  "spec_version": "1.0",
  "event_id": "01...",
  "trace_id": "0123456789abcdef0123456789abcdef",
  "timestamp": "2026-08-25 12:00:00",
  "producer": "system",
  "source": "/api/v1/health",
  "type": "api.health",
  "data": {"status": "ok"}
}
```

REST Envelope 通常不含 `seq`；SSE Envelope 含按会话递增的 `seq`。

### 错误 Problem Details

`Content-Type: application/problem+json`

```json
{
  "type": "bad_request",
  "title": "请求失败",
  "status": 400,
  "detail": "message 必填",
  "instance": "/api/v1/conversations/<id>/messages"
}
```

客户端必须同时判断 HTTP 状态和业务响应，不把错误正文显示为正常回答。

### 认证

除 `/health`、`/ready` 和 `/device/ws` 外，所有 REST 端点需要 Bearer Token 认证：

```http
Authorization: Bearer <token>
```

Token 在后端首次启动时自动生成，存储在 `data/auth.token`（权限 0600）。无 Token 或错误 Token 返回 `401`。

## 1. 健康、状态与配置

| 方法 | 路径 | 认证 | 请求 | `data` 摘要 |
|---|---|---|---|---|
| GET | `/health` | ❌ 免认证 | 无 | `status`, `uptime`, `degrade` |
| GET | `/ready` | ❌ 免认证 | 无 | `status`, `checks.database/llm/wizard`；未就绪返回 503 |
| GET | `/server/status` | ✅ | 无 | `ok`, `uptime` |
| GET | `/config` | 无 | 脱敏配置摘要 |
| PATCH | `/config` | 可选 `logging`, `llm.default_model`, `server.port` | 更新后的端口、模型、日志级别 |
| GET | `/monitor/metrics` | 无 | 系统和进程指标 |
| GET | `/monitor/history` | 无 | 最近指标数组 |

注意：配置 PATCH 更新的是运行时允许字段；前端不能假设监听端口能在当前连接中立即切换。

## 2. 会话

| 方法 | 路径 | 请求 | `data` |
|---|---|---|---|
| POST | `/conversations/` | 空体可用 | 新 `Session` |
| GET | `/conversations/` | 无 | `Session[]` |
| GET | `/conversations/{id}` | 无 | `Session` |
| POST | `/conversations/{id}/messages` | `{"message":"..."}` | `{"answer":"..."}` |
| POST | `/conversations/{id}/rewind` | `{"keep":2}` | `{"ok":true}` |

`Session`：`id`, `title`, `created_at`, `updated_at`, `messages`, `mode`。
`Message`：`role`, `content`, 可选 `run_id`, `at`。

发送消息可能耗时，前端应同时订阅对应 `session_id` 的 SSE 来显示运行状态。

## 3. 知识库

| 方法 | 路径 | 请求/参数 | `data` |
|---|---|---|---|
| POST | `/kb/search` | `{"query":"...","top_k":5}` | `hits[]`，含 `score`, `match_type` |
| GET | `/kb/store?offset=0&limit=20` | 查询参数 | `entries[]` |
| POST | `/kb/store` | Knowledge Entry；`content` 必填 | `ok` |
| GET | `/kb/store/{id}` | 无 | Entry |
| PATCH | `/kb/store/{id}` | Entry 字段 | `ok` |
| DELETE | `/kb/store/{id}` | 无 | `ok` |
| POST | `/kb/compile` | `{"source":"文本或来源"}` | CompileJob |
| GET | `/kb/compile/{job_id}` | 无 | CompileJob |
| POST | `/kb/distill` | 空体 | `extracted` |
| GET | `/kb/snapshots` | 无 | `count`, `snapshots[]` |
| POST | `/kb/snapshots` | 空体 | `path`, `status` |
| POST | `/kb/rollback` | `{"snapshot":"..."}` | 回滚结果；高风险 |

`match_type` 目前可能为 `fts`、`vec`、`both` 或降级路径。编译作业包含 `id/source/status/progress/total/error/created_at/updated_at`。

## 4. LLM 与工具

| 方法 | 路径 | 请求 | `data` |
|---|---|---|---|
| GET | `/llm/providers` | 无 | 默认供应商和名称列表 |
| GET | `/llm/models` | 无 | `models[]` 及能力字段 |
| POST | `/llm/models/refresh` | 无 | 同模型列表 |
| GET | `/llm/feature-models` | 无 | 功能级模型映射 |
| PATCH | `/llm/feature-models` | `{"configs":[{"feature":"agent","provider":"...","model":"..."}]}` | 更新后映射 |
| GET | `/tools/` | 无 | `count`, `tools[]`；含 metadata 与 JSON Schema |
| POST | `/tools/acquire` | `name`, `url`, `sha256`, 可选描述/大小 | 安装路径和注册状态；高风险 |

前端应从 `/tools/` 动态展示工具及 `requires_approval/risk_tier/read_only`，不要硬编码全部工具元数据。

## 5. 权限

| 方法 | 路径 | 请求 | `data` |
|---|---|---|---|
| GET | `/permissions/` | 无 | 当前 `mode` 和 `modes[]` |
| PATCH | `/permissions/mode` | `{"mode":"plan"}` | 新模式 |

有效模式：`yolo`, `accept_edits`, `ask`, `plan`。前端必须对高风险模式给出明显提示。

## 6. 调度

| 方法 | 路径 | 请求 | `data` |
|---|---|---|---|
| GET | `/schedules/` | 无 | Task 数组 |
| POST | `/schedules/` | SchedulerTask | 新 `id` |
| PATCH | `/schedules/{id}` | SchedulerTask | 新 `id`（当前实现会替换任务） |
| DELETE | `/schedules/{id}` | 无 | `ok` |
| POST | `/schedules/{id}/trigger` | 无 | `id`, `status=triggered` |

SchedulerTask 字段：`name`, `type`, `spec`, `prompt`, `enabled`, `max_wall_ms`。当前更新操作可能生成新 ID，前端必须使用响应 ID 更新本地状态。

## 7. 设备

| 方法 | 路径 | 请求 | `data` |
|---|---|---|---|
| GET | `/device/` | 无 | `devices[]` |
| GET | `/device/{id}/status` | 无 | Device |
| GET | `/device/{id}/commands` | 无 | 该设备的审计命令 |
| POST | `/device/{id}/unbind` | 无 | `ok`；高风险 |
| POST | `/device/{id}/emergency-stop` | 无 | `ok`；高风险 |
| POST | `/device/pair` | `{"code":"..."}` | `device_id`, `paired` |

Device 包含 `id/name/model/android/capabilities/permissions/online/connected_at/last_seen/paired`。设备实时连接走 WebSocket。

## 8. 备份与升级

| 方法 | 路径 | 请求 | 说明 |
|---|---|---|---|
| GET | `/backups/` | 无 | 列出备份 |
| POST | `/backups/export` | 空体 | 创建 SQLite 快照 |
| POST | `/upgrade/check` | `url`, 可选 `sha256` | 仅返回检查语义 |
| POST | `/upgrade/apply` | `url`, `sha256` | 下载校验并替换二进制；极高风险 |

Android 第一版不应默认暴露“应用升级/数据库回滚”按钮；启用前需增加运营级认证和二次确认。

## 9. 团队协作

| 方法 | 路径 | 请求 | `data` |
|---|---|---|---|
| GET | `/teamwork/teams` | 无 | `teams[]` |
| POST | `/teamwork/triage` | `{"need":"..."}` | 分诊结果 |
| POST | `/teamwork/projects` | `project_id`, `question`, `team_id`, 可选 `tasks` | Project |
| POST | `/teamwork/projects/{id}/discuss` | `{"prompt":"..."}` | `proposals[]` |
| POST | `/teamwork/projects/{id}/conclude` | `{"verdict":"..."}` | Project |
| POST | `/teamwork/staffing` | `task_id`, `reason`, `role_card_id`, `permanent` | `decisions`, `alarm` |

注意：`TaskDependency` 的当前 Go 字段缺少 JSON tag，前端第一阶段不应依赖 `tasks` 的 snake_case 编码，直到后端契约补齐并加测试。

## 10. 路径尾斜杠

Chi Route 组的集合路径在代码中注册为 `/`。客户端建议按本文保留集合路径尾斜杠（例如 `/conversations/`, `/tools/`），避免不同 HTTP 客户端的重定向/405 差异。

## 11. Agent 审批

| 方法 | 路径 | 认证 | 请求 | `data` |
|---|---|---|---|---|
| POST | `/approvals/{run_id}` | ✅ | `{"approved":true}` 或 `{"approved":false}` | `run_id`, `approved` |

Agent 遇到需要审批的工具调用时，通过 SSE 推送 `agent.approval_requested` 事件（含 `run_id` 和 `tool_name`）。前端展示确认界面后，调用此端点提交决定：

- **批准**（`approved: true`）：Agent 恢复执行工具，继续后续步骤。
- **拒绝**（`approved: false`）：Agent 收到拒绝反馈，回到 Planning 让 LLM 换方案。

提交后 Agent 异步继续，后续状态变化通过 SSE 推送（`agent.approval_result` → `agent.step_started/finished` → `agent.run_finished`）。

注意：会话消息发送（`POST /conversations/{id}/messages`）已改为异步模式，立即返回 `run_id`，结果通过 SSE 推送。