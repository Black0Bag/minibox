# 前端交接说明

## 目标

Android APP 是 minibox 的设备侧客户端与用户界面：

- REST：配置、会话、知识库、工具、调度、设备和监控。
- SSE：接收 Agent、系统和会话事件。
- WebSocket：作为设备代理连接后端，使用 JSON-RPC 2.0。

前端**不直接访问 SQLite、迁移文件或后端内部 Go 包**。

## 建议的 Android 分层

```text
app/
├── core/network/       HTTP、SSE、WebSocket、重连、序列化
├── core/model/         API DTO、统一信封、错误模型
├── data/               Repository、缓存和本地状态
├── feature/chat/       对话、消息、Agent 状态
├── feature/knowledge/  知识库和编译任务
├── feature/device/     设备连接、能力、命令审批
├── feature/settings/   LLM、权限、服务器配置
└── ui/                 Compose 主题、通用组件和导航
```

推荐单向数据流：ViewModel 持有状态，Composable 接收不可变状态并向上发送事件。

## 首期开发顺序

1. 网络层和统一 JSON 信封解析。
2. `/health`、`/ready`、`/server/status` 连接诊断。
3. 会话列表、创建会话、发送消息。
4. SSE 订阅、事件序号去重和断线恢复。
5. 权限模式与工具审批界面。
6. WebSocket 设备连接、心跳、能力展示。
7. 知识库搜索和编译任务轮询。
8. 监控、调度、团队和备份等管理页面。

## 交互契约重点

### REST

所有成功业务响应都在统一 Envelope 的 `data` 中；不要只根据 HTTP 200 判断业务成功。错误响应应读取 `type/title/status/detail/instance`。

### SSE

- URL：`GET /api/v1/stream?session_id=<id>`。
- 请求头：重连时发送 `Last-Event-ID`。
- 事件块包含 `id`、`event`、`data`。
- `data` 是 JSON 编码的 Envelope；使用 `seq` 做顺序检查和去重。
- 网络断开时使用退避重连，不要并行创建同一 session 的多个订阅。

### WebSocket

- URL：`ws://<host>:<port>/device/ws`。
- 首个请求应为 `connect`，参数包含 `client`、`protocol`，若启用凭据则包含 `auth`。
- JSON-RPC 请求必须带唯一 `id`；响应按同一 `id` 归属。
- 设备主动消息可作为通知，不要求响应。
- 写操作在后端可能进入审批等待，前端必须展示待审批状态，而不是假设立即执行。

## 前端必须处理的状态

- 初始连接中、已连接、断线重连、认证失败、服务未就绪。
- Agent：运行中、等待审批、完成、失败、取消。
- SSE：当前序号、重连中、历史回放、同步窗口不足。
- 设备：在线、离线、能力不完整、命令超时、远端错误。
- LLM：供应商限流、服务不可用、业务失败；不能把错误文本当成正常回答。

当前后端会话 Hub 在收到 `agent.approval_requested` 后默认拒绝该工具并让 Agent 换方案，没有公开的 REST 审批提交端点。因此前端第一版只能显示该事件和拒绝结果；真正的“允许/拒绝”交互需要后端新增审批 API 后才能实现，不能先做一个无效按钮。

## 安全和无障碍

- API Key、设备凭据只进入安全存储或受控输入流程，禁止写入日志和 UI 崩溃报告。
- 所有危险设备操作都要有清晰的审批确认、目标设备和动作说明。
- Compose 交互控件触摸区域至少 48dp。
- Icon/Image 提供准确的 `contentDescription`；纯装饰图像使用 `null`。
- 标题使用 heading 语义，复杂列表项合并语义，选中/连接状态提供可读状态描述。
- 支持深色主题、字体放大、TalkBack 和键盘焦点顺序。

## 联调方式

先使用本地 Mock 或模拟设备，不要直接触发真实手机副作用。前端联调应记录：

- 请求方法、路径和脱敏参数；
- HTTP 状态与业务 `type`；
- SSE `id/seq/event`；
- WebSocket JSON-RPC `id/method/error.code`；
- 重连前后的最后序号。

详细字段以 [`api.md`](api.md)、[`sse.md`](sse.md)、[`websocket.md`](websocket.md) 为准。