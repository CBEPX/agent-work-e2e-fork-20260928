# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-04-02

### Added

- MCP stdio server mode with 4 tools: `serial_exec`, `serial_status`, `serial_enable`, `serial_raw`.
- REST HTTP server mode (`--rest`) for multi-agent concurrent access.
- Connection pool with automatic reconnect to QEMU serial UNIX sockets.
- Multi-agent support via `X-Agent-ID` header or `?agent=` parameter.
- Unique request ID (`rid`) on every request for log correlation.
- Structured JSON logging via `log/slog` with `--debug` flag for raw socket I/O.
- SQLite command history (`--db` flag) with WAL mode for concurrent access.
- `GET /history` endpoint with filtering by agent, VMID, and limit.
- `GET /stats` endpoint with per-agent aggregate statistics.
- `POST /exec` batch endpoint for executing multiple commands in sequence.
- Output cleaning: ANSI escape removal, `\r\n` normalization, command echo stripping.
- Dynamic VMID connection via `GET /connect?vmid=...`.
- systemd service unit with security hardening (NoNewPrivileges, ProtectSystem).
- Makefile with Podman-based build (no local Go toolchain required).
- golangci-lint v2 configuration with 50+ linters.
- Apache 2.0 license.

[Unreleased]: https://github.com/ioplane/serial-proxy/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/ioplane/serial-proxy/releases/tag/v1.0.0
