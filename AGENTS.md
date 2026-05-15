# Netquality

Single-binary home network quality monitor (Go 1.26): ICMP to gateway, DNS latency, HTTP/TCP path probes; SQLite persistence; debounced per-layer state; embedded web UI and JSON API. Typical deploy is Raspberry Pi + systemd.

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
