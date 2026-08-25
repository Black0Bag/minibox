# Codebase Concerns

## 1) Top Risks (Prioritized)

| Severity | Concern | Evidence | Impact | Suggested action |
|---|---|---|---|---|
| high | REST endpoints have no general user authentication middleware; safety currently relies on loopback default and device WS credentials | `internal/transport/http/server.go`, `internal/app/app.go` | Binding to LAN can expose configuration and destructive endpoints | Add explicit API authentication before non-loopback deployment |
| high | Upgrade and DB rollback endpoints can replace critical files | `internal/app/http_handlers.go`, `internal/infrastructure/upgrade/upgrade.go`, backup manager | Service/data loss if invoked incorrectly | Keep disabled/approval-gated in UI; add operator authorization and dedicated tests |
| medium | SSE history and several coordination states are in memory | `internal/transport/sse/server.go`, session/team coordinator structs | Restart loses replay/session/project state; many session IDs can retain history | Define lifecycle/eviction and persistence needs before scale-out |
| medium | Race detector evidence is missing | `.github/workflows/ci.yml`, `docs/validation.md` | Concurrency defects may escape current gates | Add Linux x86_64 race job for selected/all packages |
| medium | Large app files centralize many responsibilities | `internal/app/http_handlers.go`, `internal/app/app.go` | Merge conflicts and regression blast radius | Split handlers by API domain without changing public contracts |

## 2) Technical Debt

| Debt item | Why it exists | Where | Risk if ignored | Suggested fix |
|---|---|---|---|---|
| Hand-maintained API docs | No OpenAPI source-of-truth is present | `docs/api.md`, handler structs | Android DTO drift | Introduce OpenAPI after current contract freeze and generate/check DTOs |
| Mixed transient/durable state | Initial single-process design | sessions, SSE history, teamwork projects, audit | Restart semantics surprise frontend | Document reset behavior; persist only user-visible state that must survive |
| Global HTTP timeout vs streams | Shared `http.Server` config | `internal/transport/http/server.go` | Long SSE responses may be affected by timeout settings | Add runtime integration test; consider stream-specific server/timeouts |
| Limited aggregate coverage signal | CI executes tests but no threshold/report | `.github/workflows/ci.yml` | Silent coverage erosion | Publish coverage report first; set threshold only after baseline review |

## 3) Security Concerns

| Risk | OWASP category | Evidence | Current mitigation | Gap |
|---|---|---|---|---|
| Unauthenticated REST on non-loopback | A01 Broken Access Control | HTTP router has no auth middleware | safe default `127.0.0.1`; device WS credential | no REST auth if listen changes |
| Sensitive local YAML | A02 Cryptographic Failures / N/A local secret handling | config supports plaintext API keys | gitignore policy and local-only files | no integrated keystore/secrets manager |
| External URL downloads | A10 SSRF / supply chain | acquire/upgrade accept URLs | SHA-256 required, timeout and isolated install | URL allowlist/network policy not defined |
| Raw operational errors in API detail | A09 Security Logging/Monitoring | handlers pass some `err.Error()` to client | structured problem response | internal paths/provider text may leak; normalize public errors |

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
| `internal/app/http_handlers.go` | many API domains in one large file | 31.8 KB and recent churn | split by domain only after contract tests |
| `internal/infrastructure/llm/openai_compat.go` | provider compatibility and error semantics | repeated fixes + external variability | fixture-first tests for JSON/SSE/error bodies |
| `internal/transport/ws/server.go` | single-reader and pending-map concurrency invariants | recent refactor, 12.5 KB | preserve one read loop; run concurrency/out-of-order tests |
| storage migration/FTS/vector | schema and index invariants | new migration and regression fixes | new test DB, migration tests, backup before real DB |

## 6) `[ASK USER]` Questions

1. [ASK USER] Must the Android app ever connect to the REST API over LAN/public networks, or will it always use a trusted local/tunneled channel? This decides the REST authentication design.
2. [ASK USER] Which conversation/team/SSE state must survive backend restart for the first Android release?
3. [ASK USER] Should the current API contract be frozen into OpenAPI before Android implementation, or after the first Retrofit DTO pass?

## 7) Evidence

- Codebase scan: high-churn and largest-file sections (2026-08-25 local scan)
- `internal/transport/http/server.go`
- `internal/app/app.go`
- `internal/app/http_handlers.go`
- `internal/transport/sse/server.go`
- `internal/transport/ws/server.go`
- `internal/infrastructure/storage/fts.go`
- `.github/workflows/ci.yml`
