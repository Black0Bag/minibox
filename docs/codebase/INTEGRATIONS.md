# External Integrations

## 1) Integration Inventory

| System | Type | Purpose | Auth model | Criticality | Evidence |
|---|---|---|---|---|---|
| OpenAI-compatible providers | HTTPS API | Chat completions and model catalog | Bearer API key from local YAML | high | `internal/infrastructure/llm/openai_compat.go` |
| OpenAI-compatible embedding service | HTTPS API | Query/passage vectors | Bearer API key from local YAML | high when vector search enabled | `internal/infrastructure/llm/embedding.go` |
| SQLite | embedded DB | Knowledge, vectors, jobs, runs, config and conversation records | local filesystem permissions | high | `internal/infrastructure/storage/` |
| Android/device client | WebSocket JSON-RPC | Device registration, commands and events | device credential in WS `connect.auth` | high | `internal/transport/ws/server.go`, `internal/device/hub.go` |
| NTP servers | UDP/network | Clock offset calibration with system-time fallback | none | low | `internal/platform/timestamp/timestamp.go` |
| External tool URLs | HTTPS/file download | Optional verified tool acquisition | URL plus mandatory SHA-256 | medium | `internal/infrastructure/tools/acquire.go` |
| Upgrade URL | HTTPS/file download | Optional binary replacement | mandatory SHA-256 | high | `internal/infrastructure/upgrade/upgrade.go` |

## 2) Data Stores

| Store | Role | Access layer | Key risk | Evidence |
|---|---|---|---|---|
| SQLite main DB | durable application and knowledge state | `internal/infrastructure/storage` | migration/index/vector-dimension consistency | `storage.go`, `migrate.go`, migrations |
| FTS5 table | tokenized full-text index | SQLiteStore | trigger/backfill consistency | `fts.go`, `0006_fts_tokenized.sql` |
| Vector table/index | embedding retrieval | SQLiteStore | configured dimension must match schema | `vector.go`, `migrate.go` |
| In-memory SSE history | last 500 events per active/seen session | transport/sse | lost on restart, memory growth by session IDs | `internal/transport/sse/server.go` |
| In-memory sessions/team projects/device audit | transient coordination/UI state | app/teamwork/device | restart loses non-persisted portions | relevant package structs |

## 3) Secrets and Credentials Handling

- Credential sources: local YAML for LLM/embedding and generated local device credential files; real values are not committed.
- Integration tests read real-service credentials only from `MINIBOX_TEST_LLM_*` environment variables.
- Repository scan found no supported `.env.example`; configuration structure is documented in `docs/configuration.md`.
- Rotation lifecycle: `[ASK USER]` define production key rotation and Android secure-storage ownership before deployment.

## 4) Reliability and Failure Behavior

- LLM calls have configured timeouts, retry classification, provider fallback and circuit breaking.
- Embedding calls have timeout/retry handling and degrade to non-vector retrieval when unavailable.
- Tool acquisition and upgrades require SHA-256 verification; missing/mismatched hashes fail closed.
- NTP failure degrades to system time.
- WebSocket pending requests fail on context cancellation or connection close; SSE reconnect uses bounded history.

## 5) Observability for Integrations

- Structured slog records provider fallback, LLM response summary, device lifecycle and integration errors.
- Performance endpoints expose CPU, memory, disk and Go process metrics; there is no Prometheus exporter or distributed trace backend in the scan.
- Envelope carries `trace_id`, but end-to-end propagation to all external calls is partial/undocumented.
- Missing visibility: per-provider latency/error counters and durable WS/SSE connection metrics.

## 6) Evidence

- `internal/config/config.go`
- `internal/infrastructure/llm/openai_compat.go`
- `internal/infrastructure/llm/embedding.go`
- `internal/infrastructure/storage/`
- `internal/transport/ws/server.go`
- `internal/transport/sse/server.go`
- `internal/platform/timestamp/timestamp.go`
- `internal/monitor/monitor.go`
