# minibox API 文档

> **基础地址**: `http://<host>:8086`
> **响应格式**: JSON（RFC 7807 错误信封）
> **字符编码**: UTF-8

---

## 通用说明

### 统一信封格式

**成功响应**:
```json
{
  "status": "ok",
  "type": "api.xxx.yyy",
  "data": { ... }
}
```

**错误响应** (RFC 7807):
```json
{
  "status": "error",
  "type": "bad_request",
  "title": "请求失败",
  "detail": "message 必填",
  "instance": "/api/v1/conversations/xxx"
}
```

---

## 1. 健康检查

### `GET /api/v1/health` — 存活探针

进程级检查，不依赖外部资源（适用于 Kubernetes liveness probe）。

### `GET /api/v1/ready` — 就绪探针

验证所有核心依赖（数据库、LLM、向导）是否就绪。

---

## 2. 服务器状态与配置

### `GET /api/v1/server/status` — 服务器状态
### `GET /api/v1/config` — 获取配置摘要
### `PATCH /api/v1/config` — 更新配置（热重载）

---

## 3. 对话域

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/conversations` | 创建会话 |
| GET | `/api/v1/conversations` | 列出会话 |
| GET | `/api/v1/conversations/{id}` | 获取会话 |
| POST | `/api/v1/conversations/{id}/messages` | 发送消息 |
| POST | `/api/v1/conversations/{id}/rewind` | 回退会话 |

---

## 4. 知识库域

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/kb/search` | 检索知识库 |
| GET | `/api/v1/kb/store` | 分页列出条目 |
| POST | `/api/v1/kb/store` | 写入条目 |
| GET | `/api/v1/kb/store/{id}` | 获取单条 |
| PATCH | `/api/v1/kb/store/{id}` | 更新条目 |
| DELETE | `/api/v1/kb/store/{id}` | 删除条目 |
| POST | `/api/v1/kb/compile` | 提交编译作业 |
| GET | `/api/v1/kb/compile/{job_id}` | 查询编译状态 |
| POST | `/api/v1/kb/distill` | 执行蒸馏 |
| GET | `/api/v1/kb/snapshots` | 列出快照 |
| POST | `/api/v1/kb/snapshots` | 创建快照 |
| POST | `/api/v1/kb/rollback` | 回滚快照 |

---

## 5. LLM 域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/llm/providers` | 列出供应商 |
| GET | `/api/v1/llm/models` | 列出模型 |
| POST | `/api/v1/llm/models/refresh` | 刷新模型列表 |
| GET | `/api/v1/llm/feature-models` | 获取功能级模型配置 |
| PATCH | `/api/v1/llm/feature-models` | 更新功能级模型配置 |

---

## 6. 工具域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/tools` | 列出已注册工具 |
| POST | `/api/v1/tools/acquire` | 获取外部工具（B8） |

---

## 7. 权限域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/permissions` | 获取当前权限配置 |
| PATCH | `/api/v1/permissions/mode` | 切换权限模式 |

---

## 8. 调度域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/schedules` | 列出调度任务 |
| POST | `/api/v1/schedules` | 创建调度任务 |
| PATCH | `/api/v1/schedules/{id}` | 更新调度任务 |
| DELETE | `/api/v1/schedules/{id}` | 删除调度任务 |
| POST | `/api/v1/schedules/{id}/trigger` | 立即触发 |

---

## 9. 备份域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/backups` | 列出备份 |
| POST | `/api/v1/backups/export` | 创建快照 |

---

## 10. 自升级域

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/upgrade/check` | 检查更新 |
| POST | `/api/v1/upgrade/apply` | 执行升级 |

---

## 11. 性能监控域（Phase 3.5）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/monitor/metrics` | 实时系统指标 |
| GET | `/api/v1/monitor/history` | 历史指标 |

---

## 12. 团队协作域（T 系列）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/teamwork/teams` | 列出常设团队 |
| POST | `/api/v1/teamwork/triage` | 前台分诊 |
| POST | `/api/v1/teamwork/projects` | 启动项目 |
| POST | `/api/v1/teamwork/projects/{id}/discuss` | 驱动讨论 |
| POST | `/api/v1/teamwork/projects/{id}/conclude` | 结案 |
| POST | `/api/v1/teamwork/staffing` | 人力增援审批 |

---

## 13. WebSocket 端点

`ws://<host>:8086/device/ws` — 设备代理通道

---

## 14. SSE 事件流

`GET /api/v1/stream?session_id=<id>` — 订阅事件流