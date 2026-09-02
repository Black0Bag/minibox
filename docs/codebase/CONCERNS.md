# Codebase Concerns

> 最后复核：2026-09-02。已关闭项保留在表中并标注，便于追溯。

## 1) Top Risks (Prioritized)

| Severity | Concern | Evidence | Impact | Suggested action |
|---|---|---|---|---|
| ~~high~~ **已关闭 2026-09-02** | ~~REST endpoints have no general user authentication middleware~~ | `internal/transport/http/auth.go` | — | Bearer Token 中间件已实现并收敛为唯一入口；401 符合 RFC 9110/6750/7807；常量时间比较；9 个用例覆盖 |
| high | Upgrade and DB rollback endpoints can replace critical files | `internal/app/http_handlers.go`, `internal/infrastructure/upgrade/upgrade.go`, backup manager | Service/data loss if invoked incorrectly | Keep disabled/approval-gated in UI; add operator authorization and dedicated tests |
| medium | SSE history and several coordination states are in memory | `internal/transport/sse/server.go`, session/team coordinator structs | Restart loses replay/project state; many session IDs can retain history | Define lifecycle/eviction and persistence needs before scale-out（会话消息本身已落 `conversation_log`） |
| ~~medium~~ **已关闭 2026-09-02** | ~~Race detector evidence is missing~~ | `.github/workflows/ci.yml` race job | — | Linux x86_64 race job 已通过；本轮据其日志修复 5 处 data race |
| medium | Large app files centralize many responsibilities | `internal/app/http_handlers.go`（1030 行）, `internal/app/app.go`（732 行） | Merge conflicts and regression blast radius | Split handlers by API domain without changing public contracts |
| medium | 浏览器代理设计与实现不一致 | `internal/transport/ws/handlers.go`（仅方法常量 + 占位 handler）, `docs/browser-frontend.md` | Agent 无法调用浏览器能力：无对应工具、Hub 无下发路径，且占位 handler 的调用方向与设计相反 | 按 browser-frontend.md 补 BrowserTools + Hub 下发链路，或明确标注为未实装 |

## 2) Technical Debt

| Debt item | Why it exists | Where | Risk if ignored | Suggested fix |
|---|---|---|---|---|
| Hand-maintained API docs | No OpenAPI source-of-truth is present | `docs/api.md`, handler structs | Android DTO drift | Introduce OpenAPI after current contract freeze and generate/check DTOs |
| Mixed transient/durable state | Initial single-process design | sessions, SSE history, teamwork projects, audit | Restart semantics surprise frontend | Document reset behavior; persist only user-visible state that must survive |
| Global HTTP timeout vs streams | Shared `http.Server` config | `internal/transport/http/server.go` | Long SSE responses may be affected by timeout settings | Add runtime integration test; consider stream-specific server/timeouts |
| Limited aggregate coverage signal | CI executes tests but no threshold/report | `.github/workflows/ci.yml` | Silent coverage erosion | Publish coverage report first; set threshold only after baseline review |
| 编译作业仍在内存 map | 注释自认 Phase 3 落表未做 | `internal/infrastructure/storage/compiler.go` | 重启丢失编译进度，前端轮询到不存在的 job | 落表或明确文档化重启语义 |

## 3) Security Concerns

| Risk | OWASP category | Evidence | Current mitigation | Gap |
|---|---|---|---|---|
| ~~Unauthenticated REST on non-loopback~~ **已关闭 2026-09-02** | A01 Broken Access Control | `internal/transport/http/auth.go` | Bearer Token 中间件 + 白名单仅 health/ready/device-ws；常量时间比较 | 单一长期 Token，无轮换/多客户端区分；Web 端 SSE 认证方式待设计 |
| Sensitive local YAML | A02 Cryptographic Failures / N/A local secret handling | config supports plaintext API keys | gitignore policy and local-only files | no integrated keystore/secrets manager |
| External URL downloads | A10 SSRF / supply chain | acquire/upgrade accept URLs | SHA-256 required, timeout and isolated install | URL allowlist/network policy not defined |
| Raw operational errors in API detail | A09 Security Logging/Monitoring | handlers pass some `err.Error()` to client | 统一 Problem Details 结构 | internal paths/provider text may leak; normalize public errors |

## 4) Performance and Scaling Concerns

| Concern | Evidence | Current symptom | Scaling risk | Suggested improvement |
|---|---|---|---|---|
| SQLite single-writer / one configured connection | config defaults and storage code | appropriate for local single-user mode | concurrent heavy ingest can queue | retain bounded jobs; benchmark before changing pool |
| FTS rebuild on application startup | `internal/app/app.go`, `storage/fts.go` | startup work grows with knowledge size | long startup on large DB | track migration/backfill completion and rebuild only when required |
| In-memory SSE history per session | `transport/sse/server.go` | up to 500 events per session | unbounded session-key count | expiry/LRU and metrics |
| Large tool outputs/context | engine/tool caps | bounded but still memory/token intensive | high concurrent Agent load | measure allocations and enforce per-run concurrency budgets |

## 5) Fragile/High-Churn Areas

| Area | Why fragile | Churn signal | Safe change strategy |
|---|---|---|---|
| `internal/app/app.go` | startup order and every module meet here | scan shows repeated recent changes | small wiring changes, App integration test, full gate |
| `internal/app/http_handlers.go` | many API domains in one large file | 1030 行且持续变更 | split by domain only after contract tests |
| `internal/app/session.go` | 异步状态机 + 并发读写 + 持久化交织 | 2026-09-02 修出 3 个真实缺陷 | 改动必跑并发用例；对外只交快照副本 |
| `internal/infrastructure/llm/openai_compat.go` | provider compatibility and error semantics | repeated fixes + external variability | fixture-first tests for JSON/SSE/error bodies |
| `internal/transport/ws/server.go` | single-reader、pending-map、通知 goroutine 三重并发约束 | 2026-09-02 加了 Client 元数据锁 | preserve one read loop; 通知回调结果用 channel 交接，不用共享变量 |
| storage migration/FTS/vector | schema and index invariants | new migration and regression fixes | new test DB, migration tests, backup before real DB |

## 6) `[ASK USER]` Questions

1. ~~[ASK USER] REST 认证设计~~ — 已定：固定 Bearer Token（`data/auth.token`）。
   遗留子问题：是否需要 Token 轮换机制与多客户端独立凭据？
2. [ASK USER] Which conversation/team/SSE state must survive backend restart for the first Android release?（会话消息已持久化；团队项目与 SSE 历史仍在内存）
3. [ASK USER] Should the current API contract be frozen into OpenAPI before Android implementation, or after the first Retrofit DTO pass?
4. [ASK USER] 浏览器代理（前端 WebView 执行 + 后端下发）在哪个阶段实装？

## 7) Evidence

- Codebase scan: high-churn and largest-file sections (2026-08-25 local scan)
- `internal/transport/http/server.go`、`internal/transport/http/auth.go`
- `internal/app/app.go`、`internal/app/session.go`
- `internal/app/http_handlers.go`
- `internal/transport/sse/server.go`
- `internal/transport/ws/server.go`
- `internal/infrastructure/storage/fts.go`
- `.github/workflows/ci.yml`（2026-09-02 四 job 全绿）
- `docs/validation.md`（2026-09-02 加固记录）
