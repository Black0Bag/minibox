# 设备 WebSocket / JSON-RPC 契约

## 连接地址

```text
ws://<host>:<port>/device/ws
```

默认端口来自 `server.port`。WebSocket 使用文本 JSON 帧和 JSON-RPC 2.0。

## 首帧握手

客户端连接后发送 `connect`：

```json
{
  "jsonrpc": "2.0",
  "id": "connect-1",
  "method": "connect",
  "params": {
    "client": "android-device",
    "protocol": "1.0",
    "auth": "<device-token>"
  }
}
```

成功响应：

```json
{
  "jsonrpc": "2.0",
  "id": "connect-1",
  "result": {"ok": true, "protocol": "1.0"}
}
```

如果服务端启用了凭据校验，`auth` 必须通过；认证失败不会建立可用设备会话。协议版本不是 `1.0` 时拒绝。

## 通用请求/响应

请求：

```json
{"jsonrpc":"2.0","id":"42","method":"heartbeat.ping","params":{}}
```

成功：

```json
{"jsonrpc":"2.0","id":"42","result":{...}}
```

失败：

```json
{"jsonrpc":"2.0","id":"42","error":{"code":-32004,"message":"...","data":{...}}}
```

应用错误码：

| code | 含义 |
|---:|---|
| `-32001` | 尚未完成握手 |
| `-32002` | 认证失败 |
| `-32003` | 设备离线 |
| `-32004` | 超时 |
| `-32005` | 限流 |

通知不带 `id`，服务端不会返回响应。客户端发送并发请求时必须使用唯一 `id`，响应按 `id` 归属，不能按到达顺序归属。

## 设备注册消息

完成 transport 握手后，设备通过 `device` 外层方法发送内层设备消息（与应用级集成测试一致）：

```json
{
  "jsonrpc":"2.0",
  "id":"hello-1",
  "method":"device",
  "params":{
    "method":"hello",
    "params":{
      "id":"device-1",
      "model":"...",
      "android":"...",
      "capabilities":["screen","input"],
      "permissions":{}
    }
  }
}
```

同一外层结构的内层 `method` 还支持 `connect`、`disconnect`、`event`。设备状态字段包括 `id`、`model`、`android`、`capabilities`、`permissions`、`online`、`connected_at` 和 `last_seen`。

## 命令回执

后端向设备发送命令时，命令请求的 `id` 会被记录并等待同 ID 的响应：

```json
{
  "jsonrpc":"2.0",
  "id":"cmd-123",
  "method":"设备_截屏",
  "params":{"device_id":"device-1","quality":"low"}
}
```

设备应返回：

```json
{
  "jsonrpc":"2.0",
  "id":"cmd-123",
  "result":{"screen":"..."}
}
```

连接断开或超时会使等待中的命令失败；后端同时写入设备审计记录。真实设备副作用必须在单独授权的环境中验证。

## 设备方法分组

传输层支持以下方法族：

- `device.*`
- `browser.*`
- `peer.*`
- `event.*`
- `system.*`
- `heartbeat.*`

设备工具层当前提供 18 项能力。具体名称和风险元数据以 REST 的 `/api/v1/tools/` 返回为准。只读能力通常无需审批；点击、输入、启动应用、通知、朗读等写操作需要审批。

## Android 客户端实现要求

1. 维护一个连接状态机：Disconnected → Connecting → Handshaking → Ready → Reconnecting。
2. 为每条请求生成唯一 ID，并维护 pending map。
3. 读循环只能有一个；写入可以排队或串行化。
4. 心跳失败后主动重连，并重新发送握手和设备信息。
5. 不在日志中打印 `auth`、完整参数或屏幕/剪贴板敏感内容。
6. 危险动作在 UI 中展示目标设备、动作、参数摘要和审批结果。