# 配置与运行

## 配置加载

入口是 `cmd/minibox/main.go`：

```bash
./bin/minibox --config /path/to/minibox.yaml
```

配置文件由 `internal/config` 使用默认值 + YAML 覆盖加载。配置文件应放在版本控制之外，并限制为当前用户可读写。

## 配置分组

| 分组 | 主要字段 | 说明 |
|---|---|---|
| `server` | `port`, `listen`, `read_timeout`, `write_timeout`, `idle_timeout`, `shutdown_timeout` | HTTP 监听和生命周期 |
| `database` | `path`, `max_open_conns`, `max_idle_conns`, `busy_timeout` | SQLite 路径和连接参数 |
| `logging` | `level`, `format`, `output`, 轮转参数 | slog 输出 |
| `llm` | `providers`, `default_provider`, `default_model`, `feature_models` | Chat/Agent/编译/蒸馏模型路由 |
| `memory` | `compile_batch_size`, `max_tokens` | 知识编译参数 |
| `embedding` | `base_url`, `api_key`, `model`, `dimensions`, `timeout` | 可选向量服务 |
| `auth` | `token_file` | Bearer Token 文件路径，默认 `data/auth.token` |

## 安全要求

- 真实 `api_key` 只存在本地权限受限配置，不进入 Git、README、日志、测试报告或记忆。
- 不要把生产配置复制到源码仓库。
- `listen` 默认是 `127.0.0.1`。改变为局域网地址前，必须确认网络边界、凭据和防火墙策略。
- 配置中的数据库路径会影响启动时的迁移、FTS 回填和向量维度校验；不要让测试进程共用生产数据库。

## 当前稳定验证实例

当前工作区保留一份稳定验证配置及其运行数据，位于工作区外部写入路径下的测试实例目录。该配置含用户隐私凭据，本文不复制其具体内容，也不提供其完整路径或 Key 值。

## 常见问题

### 启动时报单实例锁

说明同一数据库目录仍有进程占用 `minibox.lock`。先确认目标实例和进程，再停止进程；不要直接删除锁文件掩盖真实运行进程。

### 没有 Embedding 配置

服务会降级到 FTS5/LIKE 路径。需要语义检索时，配置与模型维度必须和数据库 schema 元数据一致。

### 外部 LLM 返回 429

这是供应商限流或配额问题，不等价于源码缺陷。应查看日志中的错误类别，并与本地 Mock/回归测试结果区分。