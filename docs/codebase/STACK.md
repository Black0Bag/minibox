# Technology Stack

## 1) Runtime Summary

| Area | Value | Evidence |
|---|---|---|
| Primary language | Go | `go.mod`, `cmd/minibox/main.go` |
| Runtime + version | Go 1.26.6 | `go.mod`, `.github/workflows/ci.yml` |
| Package manager | Go modules | `go.mod`, `go.sum` |
| Module/build system | `go build`; GoReleaser v2 for releases | `.goreleaser.yaml`, `.github/workflows/release.yml` |
| Deployment unit | Single cross-platform binary, `CGO_ENABLED=0` in release | `.goreleaser.yaml` |

## 2) Production Frameworks and Dependencies

| Dependency | Version | Role in system | Evidence |
|---|---:|---|---|
| `go-chi/chi/v5` | 5.3.1 | HTTP router and middleware | `go.mod`, `internal/transport/http/server.go` |
| `coder/websocket` | 1.8.15 | WebSocket / JSON-RPC transport | `go.mod`, `internal/transport/ws/server.go` |
| `modernc.org/sqlite` | 1.56.0 | Pure-Go SQLite, migrations, FTS/vector persistence | `go.mod`, `internal/infrastructure/storage/storage.go` |
| `knadh/koanf/v2` | 2.3.6 | Default + YAML configuration loading | `go.mod`, `internal/config/config.go` |
| `lengzhao/jiebago` | 0.2.0 | Chinese tokenization for FTS index/query | `go.mod`, `internal/infrastructure/storage/tokenizer.go` |
| `sony/gobreaker/v2` | 2.4.0 | LLM provider circuit breaker | `go.mod`, `internal/infrastructure/llm/router.go` |
| `robfig/cron/v3` | 3.0.1 | In-process schedule/alarm execution | `go.mod`, `internal/infrastructure/scheduler/cron.go` |
| `oklog/ulid/v2`, `google/uuid` | 2.1.2 / 1.6.0 | Event IDs, trace/idempotency identifiers | `go.mod`, `internal/transport/envelope.go` |
| `lumberjack.v2` | 2.2.1 | Rotating file log output | `go.mod`, `internal/platform/logging/logging.go` |
| `x/sync` | 0.22.0 | Bounded concurrent orchestration | `go.mod`, `internal/infrastructure/engine/orchestrator.go` |

## 3) Development Toolchain

| Tool | Purpose | Evidence |
|---|---|---|
| `gofmt` | Standard formatting | Go source; `docs/rules.md` |
| `go test` | Unit/integration test runner | `.github/workflows/ci.yml` |
| `go vet` | Standard static analysis | `.github/workflows/ci.yml` |
| golangci-lint 2.12.2 | `govet`, `errcheck`, `staticcheck`, `unused`, `ineffassign`, `revive`, `misspell`, `gocritic` | `.golangci.yml`, `.github/workflows/ci.yml` |
| govulncheck | Dependency vulnerability scan | `.github/workflows/ci.yml` |
| GoReleaser v2 | Cross-platform release archives/checksums | `.goreleaser.yaml`, `.github/workflows/release.yml` |

## 4) Key Commands

```bash
go mod download
go build ./cmd/minibox/
go test -count=1 ./...
go vet ./...
golangci-lint run
govulncheck ./...
```

## 5) Environment and Config

- Config sources: `internal/config/config.go` defaults plus an optional YAML passed with `--config`.
- Integration-test env vars: `MINIBOX_TEST_LLM_BASE_URL`, `MINIBOX_TEST_LLM_API_KEY`, `MINIBOX_TEST_LLM_MODEL`.
- Runtime constraints: SQLite schema/vector dimension must match the configured embedding dimension; default listen address is loopback.
- Containers/orchestration: none are present in the repository scan.

## 6) Evidence

- `go.mod`
- `cmd/minibox/main.go`
- `internal/config/config.go`
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `.goreleaser.yaml`
- `.golangci.yml`
