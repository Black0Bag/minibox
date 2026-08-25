# Coding Conventions

## 1) Naming Rules

| Item | Rule | Example | Evidence |
|---|---|---|---|
| Files | lowercase; underscore for meaningful compound names | `openai_compat.go`, `memory_gate.go` | `internal/infrastructure/` |
| Functions/methods | Go mixedCaps; exported identifiers start uppercase | `NewCompiler`, `SendRequest`, `handleReady` | `internal/app/http_handlers.go` |
| Types/interfaces | exported PascalCase; interfaces describe capability | `RPCRequester`, `KnowledgeExtractor`, `Store` | `internal/transport/rpc.go` |
| Constants/env vars | Go constants mixedCaps/PascalCase; environment variables SCREAMING_SNAKE_CASE | `SpecVersion`, `MINIBOX_TEST_LLM_MODEL` | `internal/transport/envelope.go`, `internal/app/integration_test.go` |
| JSON fields | explicit lower_snake_case tags on public DTOs | `session_id`, `max_wall_ms` | `internal/app/session.go`, `internal/app/http_handlers.go` |

## 2) Formatting and Linting

- Formatter: standard `gofmt`; no custom formatter config.
- Linter: golangci-lint v2 configuration in `.golangci.yml`.
- Enabled rules: `govet`, `errcheck`, `staticcheck`, `unused`, `ineffassign`, `revive`, `misspell`, `gocritic`.
- Commands: `gofmt -l internal`, `go vet ./...`, `golangci-lint run`.

## 3) Import and Module Conventions

- Standard library imports precede third-party/project imports; `gofmt` maintains grouping/alignment.
- Project imports are absolute module paths, never filesystem-relative imports.
- Aliases are used where package names collide or layer is material (`infrallm`, `infrasched`, `httptransport`).
- Public APIs are intentionally limited by Go `internal/`; the repository is an application, not a public library.

## 4) Error and Logging Conventions

- Errors are returned and wrapped with context; normal failures do not panic.
- REST errors are rendered as structured problem details; successful responses use the shared Envelope.
- Boundaries log once with `log/slog`; structured key/value fields are preferred.
- API keys, auth headers, device credentials and private payloads must not be logged or documented.
- External API success requires status, Content-Type and payload validation.

## 5) Testing Conventions

- Tests are co-located as `*_test.go` and use Go's `testing` package.
- Mocks are usually small hand-written interfaces or `httptest.Server` fixtures.
- SQLite tests use temporary databases; device tests use simulated WebSocket peers.
- No numeric coverage threshold is configured: `[TODO]` decide whether a CI coverage floor is useful after frontend integration stabilizes.

## 6) Evidence

- `.golangci.yml`
- `.github/workflows/ci.yml`
- `internal/app/http_handlers.go`
- `internal/platform/logging/logging.go`
- `internal/platform/errors/errors.go`
- `docs/rules.md`
- `docs/test-inventory.md`
