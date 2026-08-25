# Testing Patterns

## 1) Test Stack and Commands

- Primary test framework: Go standard `testing` package (Go 1.26.6 toolchain).
- Assertion/mocking tools: standard `t.Fatal/Error`, hand-written fakes, `httptest`, `coder/websocket`; no required third-party test framework in repository tests.
- Commands:

```bash
go test -count=1 ./...
go test -count=1 ./internal/arch/
MINIBOX_TEST_LLM_BASE_URL=... MINIBOX_TEST_LLM_API_KEY=... MINIBOX_TEST_LLM_MODEL=... go test ./internal/app/ -run Integration -v
go test -cover ./...
```

`go test -race ./...` is intended for a ThreadSanitizer-capable Linux x86_64 environment; current Android/arm64 validation could not provide that evidence.

## 2) Test Layout

- Test files are co-located with production packages and named `*_test.go`.
- Current inventory: 58 test source files under `internal/`.
- Integration helpers are explicit, e.g. `device_ws_test_helpers_test.go`; real LLM tests skip unless env vars are present.
- SQLite isolation uses `t.TempDir()`/temporary DBs; HTTP/WS use in-process test servers.

## 3) Test Scope Matrix

| Scope | Covered? | Typical target | Notes |
|---|---|---|---|
| Unit | yes | domain rules, tools, parsing, retry, migrations | Co-located standard Go tests |
| Integration | yes | App+SQLite, LLM HTTP fixtures, SSE/WS transport, simulated device | No real Android side effects |
| E2E | partial | REST/SSE/WS against an isolated running binary | Real provider calls are rate/availability dependent |
| Architecture | yes | forbidden cross-layer imports | `internal/arch/arch_test.go` |
| Race | environment-blocked | concurrency-sensitive packages | CI currently also says race disabled |

## 4) Mocking and Isolation Strategy

- Interfaces are mocked with small structs; external HTTP behavior is modeled with `httptest.Server`.
- Device E2E uses a simulated WebSocket peer that completes authenticated connect/hello and correlated command responses.
- Tests create isolated databases/directories and register cleanup callbacks.
- Common failure mode: external service rate limits or nonstandard OpenAI-compatible response shapes; fixture regressions must remain the deterministic source of truth.

## 5) Coverage and Quality Signals

- Coverage tool: Go `-cover`; no enforced threshold is configured.
- Current reported coverage: `[TODO]` no fresh aggregate percentage is recorded in this repository.
- Quality gates: go test, go vet, architecture test, golangci-lint and govulncheck in CI.
- Known gaps: real Android side effects, binary self-upgrade execution, main DB restore, ThreadSanitizer evidence and long-duration load tests.

## 6) Evidence

- `.github/workflows/ci.yml`
- `docs/test-inventory.md`
- `internal/app/integration_test.go`
- `internal/app/device_ws_integration_test.go`
- `internal/transport/sse/server_http_reconnect_test.go`
- `internal/arch/arch_test.go`
