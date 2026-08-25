# minibox 后端收尾实施计划

> 计划级别：L3（涉及权限、外部接口、数据库行为变更）
> 状态：待用户确认后执行
> 更新时间：2026-08-25 21:25

## 一、任务目标

补齐后端四个核心缺口：
1. **REST API 认证**：固定 Bearer Token，防止未授权访问
2. **Agent 审批 API**：前端可提交批准/拒绝，Agent 继续执行
3. **会话持久化**：重启后端后会话不丢失
4. **浏览器前端交叉互动**：已通过文档完成设计

---

## 二、任务拆解

### P0-1：REST API Bearer Token 认证

**目标**：给所有 API（除 `/health`、`/ready`）加一把"钥匙锁"，前端必须携带有效 Token 才能访问。

**改动文件**：
| 文件 | 改动 |
|---|---|
| `internal/config/config.go` | 新增 `Auth` 配置结构 + `Default` 中生成随机 Token |
| `internal/app/middleware.go` | 新建文件，实现 `AuthMiddleware` |
| `internal/transport/http/server.go` | 在 `buildRouter()` 中挂载中间件 |
| `cmd/minibox/main.go` | 启动时若配置无 Token 则自动生成并持久化到文件 |

**验证方式**：
```bash
# 无 token 应返回 401
curl http://127.0.0.1:8122/api/v1/status

# 带 token 应正常返回
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8122/api/v1/status
```

**回滚**：移除 `buildRouter()` 中的 `AuthMiddleware` 挂载即可恢复无认证状态。

---

### P0-2：Agent 审批提交 API

**目标**：前端收到 `agent.approval_requested` 事件后，用户可点击"允许/拒绝"，通过 API 通知后端继续执行。

**改动文件**：
| 文件 | 改动 |
|---|---|
| `internal/app/http_handlers.go` | 新增 `handleApproval` 处理器 |
| `internal/app/app.go` | 在 `mountREST` 中注册路由 `/api/v1/approvals/{run_id}` |
| `internal/app/session.go` | 暴露 `GetRun` 方法供 handler 查询（或复用现有 Agent 引用） |
| `internal/domain/agent/engine.go` | 确保 `Approve` 方法对 handler 可见（已有） |

**请求格式**：
```
POST /api/v1/approvals/run_xxx
{
  "approved": true,
  "tool_call_id": "call_xxx"
}
```

**返回**：
```json
{
  "ok": true,
  "message": "审批已提交",
  "run_state": "planning"
}
```

**验证**：
1. Agent 进入等待审批状态
2. 前端调用此 API 提交批准
3. Agent 继续执行工具

**回滚**：临时禁用该路由即可。

---

### P1-1：会话持久化（SQLite 存储）

**目标**：后端重启后，之前的会话和消息不丢失。

**改动文件**：
| 文件 | 改动 |
|---|---|
| `internal/infrastructure/storage/migrate.go` | 新增迁移，创建 `conversations` 表和 `conversation_log` 表（若不存在） |
| `internal/app/session.go` | 新增 `Store` 接口，每次发送消息时同步写入 `conversation_log` |
| `internal/app/app.go` | 启动时从数据库恢复最近 N 条会话（默认 50 条） |

**存储策略**：
- 每次用户发送消息时，同步写入 `conversation_log` 表
- 启动时加载最近 50 条会话（可配置）
- 会话列表从数据库聚合查询

**验证**：
1. 创建会话并发送消息 → 重启后端 → 会话仍存在
2. `conversation_log` 表有新记录

**回滚**：若出现问题，可在配置中禁用持久化，回退到纯内存模式。

---

### P2-1：浏览器代理前端交叉互动文档

**状态**：✅ 已完成（`docs/browser-frontend.md`）

**内容**：
- 设计理念：后端不跑浏览器，能力交给前端
- 通信协议：WebSocket JSON-RPC 转发
- 前端需实现的能力清单
- 安全注意事项

---

## 三、验收节点

| 节点 | 验收标准 | 责任人 |
|---|---|---|
| 认证 | 无 token 访问任意 API 返回 401 | 后端 |
| 认证 | 正确 token 正常返回数据 | 后端 |
| 审批 | 前端可提交批准/拒绝，Agent 恢复执行 | 后端 |
| 会话持久化 | 重启后端后会话列表和消息仍存在 | 后端 |
| 浏览器文档 | 文档已生成，明确前后端分工 | ✅ 已完成 |

---

## 四、风险与回滚

| 风险 | 缓解措施 | 回滚方式 |
|---|---|---|
| 认证中间件误伤健康检查 | `/health`、`/ready` 白名单放行 | 移除中间件挂载 |
| 审批 API 被滥用 | 同样需要认证，且校验 `run_id` 是否存在 | 临时禁用该路由 |
| 会话持久化导致启动变慢 | 仅加载最近 50 条，可配置 | 配置开关禁用持久化 |
| Token 泄露 | 存储在本地文件，权限 0600，用户手动保管 | 重新生成 Token |

---

## 五、执行顺序

```
P0-1 认证中间件
    ↓
P0-2 审批 API（依赖认证）
    ↓
P1-1 会话持久化
    ↓
P2-1 浏览器文档 ✅ 已完成
```

**预计总工时**：1-2 天

---

## 六、确认清单

请确认以下内容后，我开始 P0-1 认证中间件的实施：

- [ ] 认证方案是否同意（固定 Bearer Token）？
- [ ] 审批 API 格式是否接受？
- [ ] 会话持久化策略是否接受？
- [ ] 浏览器文档是否需要补充？