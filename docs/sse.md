# SSE 事件流契约

## 连接

```text
GET /api/v1/stream?session_id=<session-id>
Accept: text/event-stream
```

服务端返回 `200` 和 `Content-Type: text/event-stream`，连接建立后会立即 Flush 响应头。未提供 `session_id` 时使用默认会话。

## 重连

客户端重连时发送：

```http
Last-Event-ID: <last-seq>
```

服务端从 `last-seq + 1` 开始回放该会话历史窗口，再发送实时事件。服务端在同一锁内完成订阅替换和历史快照，避免回放与实时发布之间出现窗口丢失或重复。

历史窗口有界；客户端若发现序号不连续，应执行一次完整状态刷新，而不是无限等待缺失事件。

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
3. 网络错误使用退避重连，并携带最后已处理的 `Last-Event-ID`。
4. `data` 解析失败时记录事件 ID 和类型，但不要把原始敏感内容写入日志。
5. 收到 `agent.state`、`agent.message.*`、`system.*`、`device.*` 等事件时按类型分发到对应状态流。