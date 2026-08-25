# Codebase Structure

## 1) Top-Level Map

| Path | Purpose | Evidence |
|---|---|---|
| `cmd/minibox/` | CLI entry, config loading, signal handling, application lifecycle | `cmd/minibox/main.go` |
| `internal/app/` | Composition root, REST handlers, sessions, device wiring | `internal/app/app.go`, `internal/app/http_handlers.go` |
| `internal/domain/` | Domain types/interfaces for Agent, LLM, memory, permissions, scheduling, teamwork and tools | package files under `internal/domain/` |
| `internal/infrastructure/` | Concrete LLM, SQLite, Agent, tools, scheduler, backup and upgrade implementations | package files under `internal/infrastructure/` |
| `internal/transport/` | Unified envelope plus HTTP, SSE and WebSocket transports | `internal/transport/` |
| `internal/device/` | Device Hub, pairing, audit, guardrails and Agent tool adapters | `internal/device/` |
| `internal/platform/` | Cross-cutting errors, event bus, retry, timestamps, logging, fs safety and process lock | `internal/platform/` |
| `internal/monitor/` | Linux/Go runtime performance metrics | `internal/monitor/monitor.go` |
| `docs/` | Current contracts, operating notes and codebase documentation | `docs/README.md` |
| `.github/workflows/` | CI and tagged release automation | `.github/workflows/*.yml` |

Generated/runtime artefacts (`build/`, `bin/`, external test-instance data) are not source-convention evidence.

## 2) Entry Points

- Main runtime entry: `cmd/minibox/main.go`.
- Secondary entry points: none; scheduler, monitor, timestamp and device heartbeat run as goroutines owned by `App.Run`.
- Entry selection: `go build ./cmd/minibox/`; runtime flags are `--config`, `--version`, and `--init`.

## 3) Module Boundaries

| Boundary | What belongs here | What must not be here |
|---|---|---|
| `cmd` | CLI flags and process lifecycle | Business rules, SQL, protocol parsing |
| `app` | Dependency wiring and cross-module translation | Reusable infrastructure algorithms |
| `domain` | Business models and narrow interfaces | Imports of app, transport or infrastructure implementation packages |
| `infrastructure` | Storage, external clients and concrete services | HTTP response concerns |
| `transport` | HTTP/SSE/WS protocol mechanics | Database or Agent business decisions |
| `device` | Device lifecycle, commands, audit and guardrails | Direct HTTP handler ownership |
| `platform` | Shared technical primitives | Feature-specific workflows |

`internal/arch/arch_test.go` guards selected import boundaries.

## 4) Naming and Organization Rules

- Files/directories use lowercase Go package names and snake_case only when a compound filename improves meaning, e.g. `memory_gate.go`, `openai_compat.go`.
- Tests are co-located and named `*_test.go`; integration intent appears in names such as `device_ws_integration_test.go`.
- Organization is primarily layered, with domain subpackages split by feature.
- Imports use module-absolute paths (`github.com/Black0Bag/minibox/internal/...`); aliases disambiguate same-name packages, e.g. `infrallm`, `httptransport`.

## 5) Evidence

- `cmd/minibox/main.go`
- `internal/app/app.go`
- `internal/arch/arch_test.go`
- `internal/domain/`
- `internal/infrastructure/`
- `internal/transport/`
- `docs/test-inventory.md`
