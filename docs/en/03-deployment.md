# Deployment

## Prerequisites

- Proxmox VE host with QEMU VMs using serial consoles (`--serial0 socket`)
- VMs must have `--vga serial0` to redirect console output to the serial socket
- Network access to PVE host (direct or via Teleport `tsh`)

## Build

```bash
# Podman-based build (no local Go toolchain needed)
make build

# Output: ./serial-proxy (static ELF binary, ~12MB)
```

## Install on Proxmox VE

### Automated

```bash
make deploy
```

This builds the binary, copies it to `/usr/local/bin/serial-proxy` on `pve-netlab`, installs the systemd unit, and starts the service.

### Manual

```bash
# Copy binary
scp serial-proxy root@pve-host:/usr/local/bin/serial-proxy
chmod +x /usr/local/bin/serial-proxy

# Create data directory
mkdir -p /var/lib/serial-proxy

# Install systemd unit
cp deployments/systemd/serial-proxy.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now serial-proxy

# Verify
systemctl status serial-proxy
curl -s http://localhost:8800/health
```

## systemd Unit

The included unit (`deployments/systemd/serial-proxy.service`) provides:

- Automatic restart on failure (5s delay)
- Structured JSON logging to journald
- Security hardening:
  - `NoNewPrivileges=true`
  - `ProtectSystem=strict`
  - `ProtectHome=true`
  - `PrivateTmp=true`
  - `ReadWritePaths=/var/lib/serial-proxy` (for SQLite history)

### Customizing VMIDs

Edit the unit's `ExecStart` line:

```ini
ExecStart=/usr/local/bin/serial-proxy --rest --vmid 700 --vmid 600 --vmid 601 --port 8800
```

Or add `--debug` for verbose logging:

```ini
ExecStart=/usr/local/bin/serial-proxy --rest --debug --vmid 700 --port 8800
```

## Monitoring

### Logs

```bash
# Follow structured logs
journalctl -u serial-proxy -f -o cat

# Filter by agent
journalctl -u serial-proxy -o cat | jq 'select(.agent == "my-agent")'

# Filter errors
journalctl -u serial-proxy -o cat | jq 'select(.level == "ERROR")'
```

### Statistics

```bash
# Per-agent command counts and average durations
curl -s http://localhost:8800/stats | jq .

# Recent command history
curl -s 'http://localhost:8800/history?limit=10' | jq .
```

## Security Considerations

- The REST API binds to `127.0.0.1` by default (localhost only).
- No authentication — access control relies on the host's network/SSH layer.
- Serial console access is equivalent to physical console access on the VM.
- The `--bind 0.0.0.0` flag is available but should only be used behind a reverse proxy with authentication.
- SQLite history may contain sensitive CLI output — restrict access to `/var/lib/serial-proxy/`.
