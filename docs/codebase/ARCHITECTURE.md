# Architecture

## 1) Architectural Style

- Primary style: layered modular monolith with an explicit composition root.
- Evidence: one Go module and binary; `internal/app.App` creates and connects domain interfaces, infrastructure implementations and transports.
- Primary constraints: pure-Go release build, local SQLite ownership, frontend access only through REST/SSE/WebSocket, and fail-closed tool/device permissions.

## 2) System Flow

```text
CLI -> App composition root -> HTTP/SSE/WS transport -> domain interface/state machine
    -> infrastructure implementation -> SQLite or external LLM/Embedding/device
    -> unified response/event/audit output
```

1. `cmd/minibox/main.go` loads config/logger and creates `app.App`.
2. `internal/app/app.go` opens/migrates SQLite, builds LLM router, tools, Agent, transports and background services.
3. REST handlers in `internal/app/http_handlers.go` validate input and call stores/services.
4. Agent requests pass through MemoryGate, FeatureRouter and the permission-controlled tool executor.
5. Storage and external calls are implemented under `internal/infrastructure/`.
6. Responses use REST Envelope, SSE Envelope events, or JSON-RPC responses/audit records.

## 3) Layer/Module Responsibilities

| Layer or module | Owns | Must not own | Evidence |
|---|---|---|---|
| `cmd` | flags, process signals, startup/shutdown | domain decisions | `cmd/minibox/main.go` |
| `app` | composition, handler translation, cross-module callbacks | reusable provider/storage logic | `internal/app/app.go` |
| `domain` | Agent and business contracts/state | concrete DB/HTTP clients | `internal/domain/agent/engine.go` |
| `infrastructure` | LLM clients, SQLite, scheduling, tools, backup/upgrade | HTTP rendering | `internal/infrastructure/` |
| `transport` | protocol parsing, envelopes, connection lifecycle | knowledge/tool policy | `internal/transport/` |
| `device` | device state, RPC commands, audit, guardrails | UI behavior | `internal/device/hub.go` |

## 4) Reused Patterns

| Pattern | Where found | Why it exists |
|---|---|---|
| Composition root / dependency injection | `internal/app/app.go` | Keeps module implementations replaceable and wiring explicit |
| Consumer-owned narrow interface | `transport.RPCClient`, storage/Agent interfaces | Decouples Hub/domain logic from concrete WS/SQLite code |
| State machine | Agent `Run.State`, compile jobs | Makes long-running transitions auditable and resumable |
| Strategy/router | LLM Router + FeatureRouter, permission policies | Chooses providers/models/policies without spreading branching |
| Repository/store | SQLiteStore, RunStore, SystemConfigStore | Centralizes persistence and transactions |
| Adapter | `embedAdapter`, device command tools, toolkit | Bridges package-specific contracts |
| Bounded history/pending map | SSE history, WS RPC pending map | Supports reconnect and concurrent response correlation |
| Fail-closed guard chain | permission policy chain, device guardrails | Prevents unsafe actions when approval/context is missing |

## 5) Known Architectural Risks

- `internal/app/http_handlers.go` and `internal/app/app.go` are large/high-churn composition files; unrelated changes can collide.
- SSE history and sessions/projects are in-memory; restart and multi-process semantics differ from persisted stores.
- HTTP write timeout is configured globally while SSE is long-lived; the runtime behavior should remain covered by integration tests.
- Upgrade and database restore have high blast radius and require separate operational authorization.

## 6) Evidence

- `cmd/minibox/main.go`
- `internal/app/app.go`
- `internal/app/http_handlers.go`
- `internal/infrastructure/engine/engine.go`
- `internal/infrastructure/storage/store.go`
- `internal/transport/sse/server.go`
- `internal/transport/ws/server.go`
- `internal/device/hub.go`
