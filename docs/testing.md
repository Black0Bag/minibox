# 测试与质量门禁

## 测试分类

| 分类 | 位置/方式 | 目的 |
|---|---|---|
| 单元测试 | 与生产 Go 文件同目录的 `*_test.go` | 验证领域、工具、存储和协议细节 |
| 集成测试 | `internal/app/*integration_test.go` 等 | 验证组合根、真实 SQLite、Mock LLM 和模拟设备 |
| 真实服务探针 | 不进入仓库的本地配置和独立实例 | 验证外部供应商兼容性，不作为唯一回归依据 |
| HTTP 冒烟 | 独立运行实例 | 验证健康、就绪、核心 REST 和 SSE 握手 |
| 架构守护 | `internal/arch/arch_test.go` | 防止模块边界倒置 |

## 常用命令

```bash
gofmt -l internal
go test -count=1 ./...
go vet ./...
go build ./cmd/minibox/
```

建议在 Linux x86_64 CI 执行：

```bash
go test -race ./...
```

## 验收判定原则

- HTTP 200 只证明传输层成功，不代表业务成功。
- 外部 API 必须同时检查状态码、Content-Type、响应结构和业务字段。
- Mock、模拟设备和真实外部服务的结果必须分开记录。
- 测试数据库、日志、二进制和配置必须与生产数据隔离。
- 真实设备副作用、升级、主数据库恢复需要单独授权。

## 当前证据

最近一次完整门禁已得到：

- `go test -count=1 ./...` 通过。
- `go vet ./...` 通过。
- 后端构建成功。
- 核心 REST、SSE 握手、WebSocket 模拟设备往返和设备工具权限已验证。
- `go test -race ./...` 受当前 Android/arm64 运行环境 ThreadSanitizer 地址空间限制，尚未取得本机证据。

## 稳定性测试建议

后续前端联调前，可在不触碰真实设备的条件下执行：

1. Mock LLM 连续调用和错误注入。
2. SSE 长连接、断线和序号回放。
3. WebSocket 并发请求与乱序回执。
4. SQLite 编译、检索、更新和删除循环。
5. Linux x86_64 CI 的 race、基准和资源观察。