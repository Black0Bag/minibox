# minibox 后端文档

本目录只保存**当前有效、可由源码或验收记录证明**的文档。历史设计过程、旧测试日志和中间结论不作为当前开发依据。

## 面向不同读者的入口

| 你要做什么 | 从这里开始 |
|---|---|
| 首次了解后端 | [`../README.md`](../README.md) |
| 启动与配置后端 | [`configuration.md`](configuration.md) |
| 对接 REST 接口 | [`api.md`](api.md) |
| 接收流式事件 | [`sse.md`](sse.md) |
| 开发 Android 设备代理 | [`websocket.md`](websocket.md) |
| 启动前端项目 | [`frontend-handoff.md`](frontend-handoff.md) |
| 理解后端架构 | [`structure.md`](structure.md) 与 [`codebase/ARCHITECTURE.md`](codebase/ARCHITECTURE.md) |
| 查看测试和限制 | [`validation.md`](validation.md)、[`testing.md`](testing.md) |
| 查看测试源文件名录 | [`test-inventory.md`](test-inventory.md) |

## 文档清单

- [`goal.md`](goal.md)：产品目标与范围边界。
- [`plan.md`](plan.md)：当前收尾、交接与下一阶段计划。
- [`rules.md`](rules.md)：开发、安全、测试和契约规则。
- [`structure.md`](structure.md)：模块边界、调用关系和高风险区域。
- [`api.md`](api.md)：REST API 契约索引和关键请求/响应。
- [`configuration.md`](configuration.md)：本地配置、凭据与运行边界。
- [`sse.md`](sse.md)：SSE 订阅和断线续传协议。
- [`websocket.md`](websocket.md)：设备 JSON-RPC WebSocket 协议。
- [`frontend-handoff.md`](frontend-handoff.md)：Android 前端交接说明。
- [`testing.md`](testing.md)：质量门禁与测试分类。
- [`test-inventory.md`](test-inventory.md)：保留的 Go 测试源文件名录。
- [`validation.md`](validation.md)：最终验收摘要和已知限制。
- [`codebase/`](codebase/)：按源码证据整理的开发者参考文档。

## 文档维护原则

1. README、架构和接口文档以当前源码为准；不要从历史进度日志复制过时结论。
2. API 或协议变更必须同步更新本目录相关文档和前端交接文档。
3. 不记录真实 Key、Token、设备凭据、私有服务 URL 或用户数据。
4. 测试报告只记录可复现结论，并明确区分 Mock、模拟设备与真实外部服务。
5. 有副作用的能力（设备写操作、升级、数据库恢复）必须明确说明审批边界。