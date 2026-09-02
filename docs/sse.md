# SSE 事件流契约

## 连接

```text
GET /api/v1/stream?session_id=<session-id>
Accept: text/event-stream
Authorization: Bearer <token>
```

服务端返回 `200` 和 `Content-Type: text/event-stream`，连接建立后会立即 Flush 响应头。未提供 `session_id` 时使用默认会话。

## 认证（必读）

`/api/v1/stream` **不在免认证白名单内**，必须携带
`Authorization: Bearer <token>` 请求头，否则返回 `401` +
`application/problem+json`（`type` 为 `unauthorized`）。

这带来一个客户端选型约束：

| 客户端 | 能否带自定义请求头 | 结论 |
|---|---|---|
| Android OkHttp + `okhttp-sse`（`EventSources.createFactory(client).newEventSource(request, listener)`） | 能（`Request.Builder().addHeader(...)`） | ✅ 推荐 |
| Kotlin/Java 自建 HTTP 长连接解析 | 能 | ✅ 可行 |
| 浏览器原生 `EventSource` | **不能**（构造函数只有 `withCredentials` 选项，MDN 已确认） | ❌ 需另设方案 |

Android 客户端按 OkHttp 路线实现即可，不受影响。若后续要做 Web 前端，
需要另行设计浏览器可用的认证方式（例如短时效一次性 ticket），
**不要**把长期 Token 放进查询参数——会泄漏到日志、Referer 和浏览器历史。

## 重连

客户端重连时发送：

```http
Last-Event-ID: <last-seq>
Authorization: Bearer <token>
```

服务端从 `last-seq + 1` 开始回放该会话历史窗口，再发送实时事件。服务端在同一锁内完成订阅替换和历史快照，避免回放与实时发布之间出现窗口丢失或重复。

历史窗口有界（每会话最近 500 条）；客户端若发现序号不连续，应执行一次完整状态刷新，而不是无限等待缺失事件。

## 事件格式

```text
id: 12
event: agent.run_started
data: {"spec_version":"1.0","event_id":"...","trace_id":"...","seq":12,"timestamp":"...","producer":"agent","source":"minibox://session/<id>","type":"agent.run_started","data":{...}}

```

字段说明：

- `id`：SSE 重连游标，当前实现等于 Envelope 的 `seq`。
- `event`：事件类型，通常与 Envelope `type` 一致。
- `data`：JSON 编码的统一 Envelope。
- `seq`：按会话递增的序号。
- `event_id`：事件唯一 ID，用于客户端幂等去重。
- `trace_id`：链路追踪 ID。

## 心跳

服务端默认每 15 秒发送 SSE 注释心跳：

```text
: keepalive

```

该行用于保活，不应被当作业务事件渲染。

## 客户端实现要求

1. 只允许同一个 session 保留一个活动订阅。
2. 按 `seq` 去重，并检测回退或跳号。
3. 网络错误使用退避重连，并携带最后已处理的 `Last-Event-ID` 与 `Authorization` 头。
4. `data` 解析失败时记录事件 ID 和类型，但不要把原始敏感内容写入日志。
5. 收到 `agent.state`、`agent.message.*`、`system.*`、`device.*` 等事件时按类型分发到对应状态流。
6. 收到 `401` 时不要无限重连：Token 无效不会随重试自愈，应上抛到连接状态并提示重新配置凭据。

## 会话事件序列（异步消息流）

`POST /conversations/{id}/messages` 立即返回 `run_id`，其后的事件顺序：

```text
agent.message.user          用户消息已入库
agent.run_started           run_id + session_id
agent.step_started          run_id + step + state
agent.step_finished         run_id + step + state
  ├─ 需审批时：agent.approval_requested  run_id + tool_name（此处暂停，
  │            等 POST /approvals/{run_id} 提交决定）
  └─ 提交后：  agent.approval_result     run_id + approved + state
               然后继续 step_started/step_finished 循环
agent.run_finished          run_id + state + steps
agent.message.assistant     run_id + content（state=done 且有回答时）
agent.run_failed            run_id + error（state=failed 时，替代上一条）
```

前端应以 `agent.run_finished` 的 `state` 作为终态判据，不要仅凭
`agent.message.assistant` 是否到达来判断成功。