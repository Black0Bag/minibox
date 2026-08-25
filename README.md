# minibox 后端

> 私有化强 Agent 系统的 Go 后端中枢。后端负责 Agent 编排、知识库、LLM 路由、工具权限、设备代理和 REST/SSE/WebSocket 通信；安卓 APP 作为设备侧眼耳口手。

## 当前状态

**后端核心阶段已完成，当前进入收尾与前端交接阶段。**

已完成并验证的核心能力：

- 多供应商 OpenAI 兼容 LLM、SSE 解析与功能级模型路由
- Embedding、SQLite、FTS5 中文检索、向量与混合检索
- Agent 状态机、MemoryGate、Plan-first 与工具权限链
- 知识编译、结构化提炼、蒸馏、快照与回滚能力
- REST、SSE 断线续传、WebSocket JSON-RPC 设备通道
- 设备 Hub、命令回执、审计和 18 个设备工具
- 调度、团队协作、监控、备份、首次启动向导和安全护栏

最终验收证据和限制见 [`docs/validation.md`](docs/validation.md)。

## 技术栈

- Go 1.26+
- 模块化单体：`cmd/` + `internal/`
- SQLite（`modernc.org/sqlite`，纯 Go）
- FTS5 + jieba 预分词 + 向量检索/RRF 混合
- Chi HTTP 路由
- WebSocket：JSON-RPC 2.0
- SSE：事件流、序号和 `Last-Event-ID` 回放
- `log/slog`、Cron 调度、GoReleaser、GitHub Actions

## 快速开始

### 1. 准备配置

项目不会提交真实凭据。请在版本控制之外创建本地 YAML 配置并填写 LLM/Embedding 信息。配置字段定义见 [`docs/configuration.md`](docs/configuration.md)。

### 2. 构建

```bash
go build -o bin/minibox ./cmd/minibox/
```

### 3. 初始化或启动

```bash
./bin/minibox --init --config /path/to/minibox.yaml
./bin/minibox --config /path/to/minibox.yaml
```

默认监听地址是 `127.0.0.1:8086`。生产或跨设备部署前，请先阅读安全说明；不要直接暴露未配置认证的 HTTP/WS 服务。

## 验证命令

```bash
gofmt -l internal
go test -count=1 ./...
go vet ./...
go build ./cmd/minibox/
```

`go test -race ./...` 建议在支持 ThreadSanitizer 的 Linux x86_64 CI 环境执行。当前 Android/arm64 开发环境无法提供该证据。

## 接口入口

| 通道 | 地址 | 用途 |
|---|---|---|
| REST | `/api/v1/*` | 配置、会话、知识库、工具、调度、设备和监控 |
| SSE | `/api/v1/stream?session_id=<id>` | Agent/系统事件流与断线续传 |
| WebSocket | `/device/ws` | 设备连接、JSON-RPC 命令和事件 |

接口契约见：

- [`docs/api.md`](docs/api.md)
- [`docs/sse.md`](docs/sse.md)
- [`docs/websocket.md`](docs/websocket.md)
- [`docs/frontend-handoff.md`](docs/frontend-handoff.md)

## 目录结构

```text
cmd/minibox/                 程序入口与生命周期
internal/app/                组合根、HTTP handler、会话和设备装配
internal/domain/             领域类型、接口和权限模型
internal/infrastructure/     SQLite、LLM、Agent、工具、调度和备份实现
internal/transport/          HTTP、SSE、WebSocket、统一信封
internal/device/             设备 Hub、配对、审计、工具适配器
docs/                        当前有效文档和前端交接契约
```

## 安全边界

- 不把 API Key、Token、设备凭据或私有 URL 写入源码、文档、日志和 Git。
- 设备写操作默认需要审批，权限链采用 fail-closed 语义。
- 真实 Android 副作用、自升级、主数据库恢复和外部平台写操作不属于普通测试命令。
- 测试生成物放在独立测试目录，不与源码和生产数据混合。

## 贡献与变更

1. 先阅读 [`docs/README.md`](docs/README.md) 和 [`docs/structure.md`](docs/structure.md)。
2. 修改前先写清影响范围、风险和回滚方式。
3. 每个修复至少补主流程、失败分支和回归测试。
4. 完成 `gofmt`、`go test`、`go vet` 和构建验证后再提交。
5. 前端只依赖已记录的 API 契约，不直接访问后端 SQLite。
