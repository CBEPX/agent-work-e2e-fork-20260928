# API Reference

## REST Endpoints

All responses are `application/json`. All endpoints accept `?agent=` or `X-Agent-ID` header for tracking.

### GET /exec

Execute a single CLI command.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `cmd` | string | **required** | CLI command to execute |
| `vmid` | int | first connected | QEMU VM ID |
| `wait` | float | 3.0 | Seconds to wait for response |
| `prompt` | string | `#` | Prompt string to detect end of output |
| `agent` | string | `anonymous` | Agent identifier |

**Response:**

```json
{
  "vmid": 700,
  "cmd": "show version",
  "output": "Cisco Adaptive Security Appliance...",
  "rid": "6b391544",
  "agent": "my-agent",
  "duration_s": 0.522
}
```

### POST /exec

Execute multiple commands in sequence on the same VM.

**Request body:**

```json
{
  "vmid": 700,
  "commands": ["show version", "show license all"],
  "wait": 3.0,
  "prompt": "#",
  "agent": "batch-runner"
}
```

**Response:**

```json
{
  "vmid": 700,
  "results": [
    {"cmd": "show version", "output": "...", "duration_s": 0.52},
    {"cmd": "show license all", "output": "...", "duration_s": 0.48}
  ],
  "rid": "a1b2c3d4",
  "agent": "batch-runner",
  "duration_s": 1.01
}
```

### GET /enable

Enter privileged/enable mode.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `vmid` | int | first connected | QEMU VM ID |
| `password` | string | empty | Enable password |

**Response:**

```json
{
  "vmid": 700,
  "output": "ciscoasa# terminal pager 0\nciscoasa#",
  "status": "enabled",
  "rid": "...",
  "agent": "..."
}
```

### GET /status

Show pool connections and available sockets.

```json
{
  "connections": {
    "700": {"connected": true, "socket": "/var/run/qemu-server/700.serial0"},
    "600": {"connected": true, "socket": "/var/run/qemu-server/600.serial0"}
  },
  "available_sockets": ["400", "500", "600", "700"]
}
```

### GET /connect?vmid=NNN

Connect to a new VM serial socket.

### GET /disconnect?vmid=NNN

Disconnect from a VM.

### GET /history

Query command execution history.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `agent` | string | all | Filter by agent |
| `vmid` | int | all | Filter by VMID |
| `limit` | int | 50 | Max entries |

### GET /stats

Aggregate statistics by agent.

```json
{
  "total_commands": 42,
  "agents": [
    {"agent": "static-analyst", "count": 30, "avg_duration_s": 0.512},
    {"agent": "license-researcher", "count": 12, "avg_duration_s": 0.634}
  ]
}
```

### GET /health

Returns `{"ok": true}`.

---

## MCP Tools

Used in MCP stdio mode. Input schemas use `jsonschema` tags.

### serial_exec

Execute a CLI command on a QEMU VM serial console.

**Input:** `{vmid, cmd, wait, prompt}`
**Output:** `{vmid, cmd, output, duration_s}`

### serial_status

Show connection status and available QEMU VM sockets.

**Input:** none
**Output:** `{connections, available_sockets}`

### serial_enable

Enter privileged/enable mode with optional password.

**Input:** `{vmid, password}`
**Output:** `{vmid, output, status}`

### serial_raw

Send raw data to a serial console.

**Input:** `{vmid, data, wait}`
**Output:** `{vmid, output}`
