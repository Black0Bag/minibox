# minibox 后端

minibox 的中枢大脑：以 Go 为语言、以知识库为唯一记忆的私有化强 Agent 后端。

> 技术路线图见 [`Black0Bag/minibox-dev`](https://github.com/Black0Bag/minibox-dev)（文档中心）。

## 技术栈

- Go 1.25+，零 CGO，跨平台（路由器 arm64 / N1 / 云 / PC）
- SQLite（modernc.org/sqlite 纯 Go）+ FTS5 + sqlite-vec 向量
- chi + SSE + WebSocket（coder/websocket）
- slog + lumberjack 日志
- 多供应商 LLM 接入

## 快速开始

```bash
# 1. 生成配置文件（首次启动向导）
minibox 配置

# 2. 编辑 config.yaml，填入你的 LLM 供应商
# providers:
#   - name: deepseek
#     base_url: https://api.example.com/v1
#     api_key: sk-xxx
#     model: deepseek-chat
#     enabled: true

# 3. 启动服务（默认端口 8086）
minibox 启动

# 4. 健康检查
curl http://<host>:8086/api/v1/healthz
```

## 当前里程碑

- **v0.1.0**（M1 地基）：工程骨架 + Phase 0（logme 留痕 / fsutil 防火墙 / 时间戳 / 日志）+ 基础 API（状态/版本/日志/供应商管理）

## 开发

```bash
go build ./...          # 编译
go vet ./...            # 静态检查
go test ./...           # 测试
```

CI（GitHub Actions）会自动执行 vet + test + lint 并发布 Release 二进制。
