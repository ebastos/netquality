# Netquality

Single-binary home network quality monitor (Go 1.26): ICMP to gateway, DNS latency, HTTP/TCP path probes; SQLite persistence; debounced per-layer state; embedded web UI and JSON API. Typical deploy is Raspberry Pi + systemd.

## Rules

### Rule 1 — Think Before Coding.
No silent assumptions. State what you're assuming. Surface tradeoffs. Ask before guessing. Push back when a simpler approach exists.

### Rule 2 — Simplicity First.
Minimum code that solves the problem. No speculative features. No abstractions for single-use code. If a senior engineer would call it overcomplicated — simplify.

### Rule 3 — Surgical Changes.
Touch only what you must. Don't "improve" adjacent code, comments, or formatting. Don't refactor what isn't broken. Match existing style.

### Rule 4 — Goal-Driven Execution.
Define success criteria. Loop until verified. Don't tell Claude what steps to follow, tell it what success looks like and let it iterate.

### Rule 5 — Surface conflicts, don't average them
If two existing patterns in the codebase contradict, don't blend them.
Pick one (the more recent / more tested), explain why, and flag the other for cleanup.
"Average" code that satisfies both rules is the worst code.

### Rule 6 — Read before you write
Before adding code in a file, read the file's exports, the immediate caller, and any obvious shared utilities.
If you don't understand why existing code is structured the way it is, ask before adding to it.
"Looks orthogonal to me" is the most dangerous phrase in this codebase.

### Rule 7 — Tests verify intent, not just behavior
Every test must encode WHY the behavior matters, not just WHAT it does.
A test like `expect(getUserName()).toBe('John')` is worthless if the function takes a hardcoded ID.
If you can't write a test that would fail when business logic changes, the function is wrong.

### Rule 8 — Checkpoint after every significant step
After completing each step in a multi-step task: summarize what was done, what's verified, what's left.
Don't continue from a state you can't describe back to me.
If you lose track, stop and restate.

### Rule 9 — Match the codebase's conventions, even if you disagree
If the codebase uses snake_case and you'd prefer camelCase: snake_case.
If the codebase uses class-based components and you'd prefer hooks: class-based.
Disagreement is a separate conversation. Inside the codebase, conformance > taste.
If you genuinely think the convention is harmful, surface it. Don't fork it silently.

### Rule 10 — Fail loud
If you can't be sure something worked, say so explicitly.
"Migration completed" is wrong if 30 records were skipped silently.
"Tests pass" is wrong if you skipped any.
"Feature works" is wrong if you didn't verify the edge case I asked about.
Default to surfacing uncertainty, not hiding it.


## Commands

- CI-equivalent checks: `go vet ./...` and `go test -race ./...`
- Fast loop: `go test ./...`
- Run daemon locally: `NETQUALITY_CONFIG=deploy/config.example.yaml go run ./cmd/netqualityd` (point `data_dir` at a writable path, e.g. `./data`)
- Local binary: `make build` → `bin/netqualityd`
- Pi cross-build (both ABIs): `make build-pi` → `bin/netqualityd-linux-armv7` and `bin/netqualityd-linux-arm64`

## Repository map

| Path | Role |
| --- | --- |
| `cmd/netqualityd` | Process entry: config, DB, scheduler goroutine, HTTP server, graceful shutdown |
| `internal/config` | YAML struct types and `Load` |
| `internal/probe` | Probes and `Runner` orchestration |
| `internal/store` | SQLite access, samples, rollups, incidents |
| `internal/eval` | Threshold / baseline evaluation and dimension state |
| `internal/baseline` | Learned baselines (warmup, recomputation) |
| `internal/scheduler` | Scheduled probe cycles and persistence |
| `internal/api` | `net/http` mux, JSON handlers, `//go:embed web/*` |
| `internal/api/web` | Static dashboard assets served from `/` |
| `deploy/` | Example config, systemd unit |

## Decisions

### YAML vs code changes

| Question | Approach |
| --- | --- |
| Tune thresholds, intervals, retention, or add another configured target row | Edit YAML only (`deploy/config.example.yaml` documents keys; production copies to `/etc/netquality/config.yaml`) |
| New HTTP surface or JSON response shape | Code in `internal/api` + tests |
| New measurement dimension, column, or state transition | Code across `internal/probe`, `internal/store`, and usually `internal/eval` |

## Workflows

### Add a JSON API endpoint

1. Add a `handlers` method in `internal/api/handlers.go` that follows neighbors: validate input, use `r.Context()`, return errors with `http.Error`, set `Content-Type: application/json`, encode with `json.NewEncoder(w)`.
2. Register the route in `internal/api/server.go` using the same style as existing routes, e.g. `mux.HandleFunc("GET /api/v1/your-path", h.yourMethod)` (stdlib `http.ServeMux` method pattern).
3. Cover behavior in `internal/api/handlers_test.go` via `httptest` and `New(cfg, db, engine).Handler()`.

### Extend configuration

1. Add fields and YAML tags to `internal/config/config.go`; extend validation inside `Load` when invalid combinations are possible.
2. Update `deploy/config.example.yaml` with commented examples so operators stay in sync.
3. Run `go test ./internal/config/...`.

## Code templates

Config path (CLI flag; else `NETQUALITY_CONFIG`; else default system path):

```go
configPath := flag.String("config", envOr("NETQUALITY_CONFIG", "/etc/netquality/config.yaml"), "path to config.yaml")
```

HTTP wiring (API routes before catch-all static files):

```go
mux.HandleFunc("GET /api/v1/status", h.status)
// ...
mux.Handle("/", fileServer)
```

Per-layer health strings (persisted and returned on the API) live as constants—use these instead of new literals in `internal/eval`:

```go
const (
	StateOK       = "ok"
	StateDegraded = "degraded"
	StateDown     = "down"
)
```

## Gotchas

- **Raw ICMP for the gateway probe.** The binary needs `CAP_NET_RAW` (`setcap cap_net_raw+ep …` or `AmbientCapabilities=` in systemd). Without it, gateway probing fails at runtime; for quick local dev you can set `gateway.enabled: false` in config.
- **`gateway.host` empty means auto-detect** default route gateway in `probe.NewRunner`; overriding in YAML is optional.

## Do / don't

- **Do not** register new routes only in `handlers.go`. **Do** add a `mux.HandleFunc("VERB /path", …)` line in `internal/api/server.go`.
- **Do not** run `go test ./...` alone and assume CI parity. **Do** run `go vet ./...` and `go test -race ./...` before calling work done (matches `.github/workflows/ci.yml`).

- **Do not** point `data_dir` at an unwritable location when running locally. **Do** set it to a repo-local directory you own for development.

- **Do not** introduce ad-hoc state strings in eval or storage. **Do** use `eval.StateOK`, `eval.StateDegraded`, and `eval.StateDown` (or the same spellings if extending adjacent packages).

## References

- [README.md](README.md) — operator install on Pi, cross-compile matrix, and API table for humans.
- [deploy/config.example.yaml](deploy/config.example.yaml) — authoritative commented config schema for this binary.
