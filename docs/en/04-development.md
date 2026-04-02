# Development

## Requirements

- **Podman** (all Go commands run in containers — no local Go toolchain needed)
- **make** (GNU Make)
- **tsh** (Teleport CLI, for `make deploy`)

## Project Layout

```
cmd/serial-proxy/       Main binary entry point
internal/serial/        Serial connection and pool (Conn, Pool)
internal/history/       SQLite command history (DB)
deployments/systemd/    systemd service unit
docs/en/                English documentation
docs/ru/                Russian documentation
```

## Build

```bash
make build       # Static binary via Podman
make all         # Build + test + lint
```

## Test

```bash
make test        # go test -race -count=1 ./...
```

## Lint

```bash
make lint        # golangci-lint with 50+ linters
```

The `.golangci.yml` configuration enforces strict code quality including:
- Security audit (`gosec` in audit mode)
- Shadow detection (`govet` shadow analyzer)
- Error wrapping (`wrapcheck`, `errorlint`, `err113`)
- Complexity limits (cyclomatic ≤15, cognitive ≤15, function ≤80 lines)
- Performance (`prealloc`, `perfsprint`, `intrange`)
- Modern Go patterns (`modernize`, `exptostd`)

## Deploy

```bash
make deploy      # Build + SCP to pve-netlab + restart systemd
```

## Code Conventions

- **Packages**: `internal/` for private implementation, no `pkg/` (nothing is public API)
- **Errors**: always wrap with context (`fmt.Errorf("connect vmid %d: %w", vmid, err)`)
- **Logging**: `log/slog` only, JSON handler, structured key-value pairs
- **Concurrency**: mutex per serial connection, pool mutex for map access
- **Naming**: lowercase unexported types in internal packages, MCP types prefixed with `mcp`

## Adding a New MCP Tool

1. Define input/output structs with `jsonschema` tags in `cmd/serial-proxy/main.go`
2. Write handler function matching `func(context.Context, *mcp.CallToolRequest, Input) (*mcp.CallToolResult, Output, error)`
3. Register with `mcp.AddTool(server, &mcp.Tool{Name: "...", Description: "..."}, handler)`
4. Add corresponding REST endpoint if needed
5. Update docs and CHANGELOG

## Adding a New REST Endpoint

1. Write handler function in `cmd/serial-proxy/main.go`
2. Register in the `mux.HandleFunc()` block inside main
3. Add to `restHelp()` endpoint list
4. Update docs and CHANGELOG
