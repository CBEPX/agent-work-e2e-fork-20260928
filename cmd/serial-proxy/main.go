// serial-proxy is an MCP + REST proxy for QEMU serial console sockets.
//
// It exposes QEMU serial UNIX sockets on Proxmox VE as both an MCP stdio
// server (for Claude Code / AI agents) and an HTTP REST API (for curl /
// scripting). Supports multiple concurrent agents with request tracking,
// structured JSON logging, and SQLite command history.
//
// Usage:
//
//	serial-proxy --rest --vmid 700 --vmid 600 --port 8800 --debug
//	serial-proxy --vmid 700   # MCP stdio mode (single client)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ioplane/serial-proxy/internal/history"
	"github.com/ioplane/serial-proxy/internal/serial"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "1.0.0"

var (
	pool      *serial.Pool   //nolint:gochecknoglobals // process-lifetime
	histDB    *history.DB    //nolint:gochecknoglobals // process-lifetime, nil if disabled
	debugMode atomic.Bool    //nolint:gochecknoglobals // set once at startup
)

// ── Helpers ──────────────────────────────────────────────────────────

func requestID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

func agentFromReq(r *http.Request) string {
	if a := r.Header.Get("X-Agent-ID"); a != "" {
		return a
	}
	if a := r.URL.Query().Get("agent"); a != "" {
		return a
	}
	return "anonymous"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func paramInt(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func paramFloat(r *http.Request, key string, def float64) float64 {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

func recordHistory(rid, agent string, vmid int, cmd, output, errMsg, method string, dur time.Duration) {
	if histDB != nil {
		go histDB.Record(rid, agent, vmid, cmd, output, errMsg, method, dur)
	}
}

// ── REST Handlers ───────────────────────────────────────────────────

func restExec(w http.ResponseWriter, r *http.Request) {
	rid := requestID()
	agent := agentFromReq(r)
	cmd := r.URL.Query().Get("cmd")
	if cmd == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing cmd parameter"})
		return
	}
	vmid := paramInt(r, "vmid", pool.DefaultVMID())
	wait := paramFloat(r, "wait", 3.0)
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		prompt = "#"
	}

	start := time.Now()
	slog.Info("request_start", "rid", rid, "agent", agent, "vmid", vmid, "cmd", cmd)

	conn, err := pool.Get(vmid)
	if err != nil {
		slog.Error("connect_error", "rid", rid, "agent", agent, "vmid", vmid, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "vmid": vmid, "rid": rid})
		return
	}

	output, err := conn.Exec(cmd, time.Duration(wait*float64(time.Second)), prompt)
	dur := time.Since(start)
	if err != nil {
		slog.Error("exec_error", "rid", rid, "agent", agent, "vmid", vmid, "error", err, "duration_s", dur.Seconds())
		recordHistory(rid, agent, vmid, cmd, "", err.Error(), "rest", dur)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "vmid": vmid, "rid": rid})
		return
	}

	slog.Info("request_done", "rid", rid, "agent", agent, "vmid", vmid, "cmd", cmd,
		"duration_s", dur.Seconds(), "output_len", len(output))
	recordHistory(rid, agent, vmid, cmd, output, "", "rest", dur)

	writeJSON(w, http.StatusOK, map[string]any{
		"vmid": vmid, "cmd": cmd, "output": output,
		"rid": rid, "agent": agent, "duration_s": dur.Seconds(),
	})
}

type batchRequest struct {
	VMID     int      `json:"vmid"`
	Commands []string `json:"commands"`
	Wait     float64  `json:"wait"`
	Prompt   string   `json:"prompt"`
	Agent    string   `json:"agent"`
}

func restExecBatch(w http.ResponseWriter, r *http.Request) {
	rid := requestID()
	var req batchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.VMID == 0 {
		req.VMID = pool.DefaultVMID()
	}
	if req.Wait == 0 {
		req.Wait = 3.0
	}
	if req.Prompt == "" {
		req.Prompt = "#"
	}
	agent := req.Agent
	if agent == "" {
		agent = agentFromReq(r)
	}
	start := time.Now()
	slog.Info("batch_start", "rid", rid, "agent", agent, "vmid", req.VMID, "commands", len(req.Commands))

	conn, err := pool.Get(req.VMID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "vmid": req.VMID, "rid": rid})
		return
	}

	type cmdResult struct {
		Cmd       string  `json:"cmd"`
		Output    string  `json:"output"`
		DurationS float64 `json:"duration_s"`
	}
	var results []cmdResult
	for _, cmd := range req.Commands {
		cmdStart := time.Now()
		output, cmdErr := conn.Exec(cmd, time.Duration(req.Wait*float64(time.Second)), req.Prompt)
		cmdDur := time.Since(cmdStart)
		if cmdErr != nil {
			results = append(results, cmdResult{Cmd: cmd, Output: "ERROR: " + cmdErr.Error(), DurationS: cmdDur.Seconds()})
			recordHistory(rid, agent, req.VMID, cmd, "", cmdErr.Error(), "rest-batch", cmdDur)
			continue
		}
		results = append(results, cmdResult{Cmd: cmd, Output: output, DurationS: cmdDur.Seconds()})
		recordHistory(rid, agent, req.VMID, cmd, output, "", "rest-batch", cmdDur)
	}

	dur := time.Since(start)
	slog.Info("batch_done", "rid", rid, "agent", agent, "vmid", req.VMID, "commands", len(req.Commands), "duration_s", dur.Seconds())

	writeJSON(w, http.StatusOK, map[string]any{
		"vmid": req.VMID, "results": results,
		"rid": rid, "agent": agent, "duration_s": dur.Seconds(),
	})
}

func restExecRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		restExec(w, r)
	case http.MethodPost:
		restExecBatch(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func restEnable(w http.ResponseWriter, r *http.Request) {
	rid := requestID()
	agent := agentFromReq(r)
	vmid := paramInt(r, "vmid", pool.DefaultVMID())
	password := r.URL.Query().Get("password")

	conn, err := pool.Get(vmid)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "vmid": vmid, "rid": rid})
		return
	}
	output, err := conn.Enable(password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "vmid": vmid, "rid": rid})
		return
	}
	status := "unknown"
	if strings.Contains(output, "#") {
		status = "enabled"
	}
	slog.Info("enable_done", "rid", rid, "agent", agent, "vmid", vmid, "status", status)
	writeJSON(w, http.StatusOK, map[string]any{"vmid": vmid, "output": output, "status": status, "rid": rid, "agent": agent})
}

func restStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"connections":       pool.Status(),
		"available_sockets": serial.AvailableSockets(),
	})
}

func restConnect(w http.ResponseWriter, r *http.Request) {
	vmid := paramInt(r, "vmid", 700)
	conn, err := pool.Get(vmid)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "vmid": vmid})
		return
	}
	slog.Info("connect", "vmid", vmid, "agent", agentFromReq(r))
	writeJSON(w, http.StatusOK, map[string]any{"vmid": vmid, "connected": conn.IsConnected(), "socket": conn.SockPath})
}

func restDisconnect(w http.ResponseWriter, r *http.Request) {
	vmid := paramInt(r, "vmid", pool.DefaultVMID())
	pool.Remove(vmid)
	slog.Info("disconnect", "vmid", vmid, "agent", agentFromReq(r))
	writeJSON(w, http.StatusOK, map[string]any{"vmid": vmid, "disconnected": true})
}

func restHistory(w http.ResponseWriter, r *http.Request) {
	if histDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "history not enabled"})
		return
	}
	entries, err := histDB.Query(r.URL.Query().Get("agent"), paramInt(r, "vmid", 0), paramInt(r, "limit", 50))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(entries), "entries": entries})
}

func restStats(w http.ResponseWriter, _ *http.Request) {
	if histDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "history not enabled"})
		return
	}
	total, agents, err := histDB.Stats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_commands": total, "agents": agents})
}

func restHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func restHelp(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "serial-proxy", "version": version,
		"endpoints": map[string]string{
			"GET  /status":     "Connection status and available sockets",
			"GET  /exec":       "Execute command (?cmd=...&vmid=...&wait=...&agent=...)",
			"POST /exec":       "Batch execute (JSON: {vmid, commands[], wait, prompt, agent})",
			"GET  /enable":     "Enter enable mode (?vmid=...&password=...)",
			"GET  /connect":    "Connect to VM (?vmid=...)",
			"GET  /disconnect": "Disconnect from VM (?vmid=...)",
			"GET  /history":    "Command history (?agent=...&vmid=...&limit=50)",
			"GET  /stats":      "Aggregate statistics",
			"GET  /health":     "Health check",
		},
		"agent_tracking": "X-Agent-ID header or ?agent= parameter",
		"debug":          debugMode.Load(),
	})
}

// ── MCP Tools ───────────────────────────────────────────────────────

type mcpExecInput struct {
	VMID   int     `json:"vmid"   jsonschema:"QEMU VM ID (default 700)"`
	Cmd    string  `json:"cmd"    jsonschema:"CLI command to execute,required"`
	Wait   float64 `json:"wait"   jsonschema:"seconds to wait for response (default 3)"`
	Prompt string  `json:"prompt" jsonschema:"prompt to detect end of output (default #)"`
}
type mcpExecOutput struct {
	VMID   int     `json:"vmid"`
	Cmd    string  `json:"cmd"`
	Output string  `json:"output"`
	DurS   float64 `json:"duration_s"`
}

func mcpExec(_ context.Context, _ *mcp.CallToolRequest, in mcpExecInput) (*mcp.CallToolResult, mcpExecOutput, error) {
	if in.VMID == 0 {
		in.VMID = 700
	}
	if in.Wait == 0 {
		in.Wait = 3.0
	}
	if in.Prompt == "" {
		in.Prompt = "#"
	}
	start := time.Now()
	conn, err := pool.Get(in.VMID)
	if err != nil {
		return nil, mcpExecOutput{}, fmt.Errorf("connect vmid %d: %w", in.VMID, err)
	}
	output, err := conn.Exec(in.Cmd, time.Duration(in.Wait*float64(time.Second)), in.Prompt)
	dur := time.Since(start)
	if err != nil {
		return nil, mcpExecOutput{}, fmt.Errorf("exec vmid %d: %w", in.VMID, err)
	}
	rid := requestID()
	slog.Info("mcp_exec", "rid", rid, "vmid", in.VMID, "cmd", in.Cmd, "duration_s", dur.Seconds())
	recordHistory(rid, "mcp", in.VMID, in.Cmd, output, "", "mcp", dur)
	return nil, mcpExecOutput{VMID: in.VMID, Cmd: in.Cmd, Output: output, DurS: dur.Seconds()}, nil
}

type mcpStatusInput struct{}
type mcpStatusOutput struct {
	Connections      map[string]serial.ConnInfo `json:"connections"`
	AvailableSockets []string                   `json:"available_sockets"`
}

func mcpStatus(_ context.Context, _ *mcp.CallToolRequest, _ mcpStatusInput) (*mcp.CallToolResult, mcpStatusOutput, error) {
	return nil, mcpStatusOutput{Connections: pool.Status(), AvailableSockets: serial.AvailableSockets()}, nil
}

type mcpEnableInput struct {
	VMID     int    `json:"vmid"     jsonschema:"QEMU VM ID (default 700)"`
	Password string `json:"password" jsonschema:"enable password"`
}
type mcpEnableOutput struct {
	VMID   int    `json:"vmid"`
	Output string `json:"output"`
	Status string `json:"status"`
}

func mcpEnable(_ context.Context, _ *mcp.CallToolRequest, in mcpEnableInput) (*mcp.CallToolResult, mcpEnableOutput, error) {
	if in.VMID == 0 {
		in.VMID = 700
	}
	conn, err := pool.Get(in.VMID)
	if err != nil {
		return nil, mcpEnableOutput{}, fmt.Errorf("connect vmid %d: %w", in.VMID, err)
	}
	output, err := conn.Enable(in.Password)
	if err != nil {
		return nil, mcpEnableOutput{}, fmt.Errorf("enable vmid %d: %w", in.VMID, err)
	}
	status := "unknown"
	if strings.Contains(output, "#") {
		status = "enabled"
	}
	return nil, mcpEnableOutput{VMID: in.VMID, Output: output, Status: status}, nil
}

type mcpRawInput struct {
	VMID int     `json:"vmid" jsonschema:"QEMU VM ID (default 700)"`
	Data string  `json:"data" jsonschema:"raw data to send,required"`
	Wait float64 `json:"wait" jsonschema:"seconds to wait (default 2)"`
}
type mcpRawOutput struct {
	VMID   int    `json:"vmid"`
	Output string `json:"output"`
}

func mcpRaw(_ context.Context, _ *mcp.CallToolRequest, in mcpRawInput) (*mcp.CallToolResult, mcpRawOutput, error) {
	if in.VMID == 0 {
		in.VMID = 700
	}
	if in.Wait == 0 {
		in.Wait = 2.0
	}
	data := strings.ReplaceAll(in.Data, `\r`, "\r")
	data = strings.ReplaceAll(data, `\n`, "\n")
	conn, err := pool.Get(in.VMID)
	if err != nil {
		return nil, mcpRawOutput{}, fmt.Errorf("connect vmid %d: %w", in.VMID, err)
	}
	output, err := conn.Raw(data, time.Duration(in.Wait*float64(time.Second)))
	if err != nil {
		return nil, mcpRawOutput{}, fmt.Errorf("raw vmid %d: %w", in.VMID, err)
	}
	return nil, mcpRawOutput{VMID: in.VMID, Output: output}, nil
}

// ── Multi-VMID flag ─────────────────────────────────────────────────

type vmidList []int

func (v *vmidList) String() string { return fmt.Sprintf("%v", *v) }
func (v *vmidList) Set(s string) error {
	i, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("parse vmid %q: %w", s, err)
	}
	*v = append(*v, i)
	return nil
}

// ── Main ────────────────────────────────────────────────────────────

func main() {
	var vmids vmidList
	rest := flag.Bool("rest", false, "Run in REST mode (multi-agent HTTP)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	dbPath := flag.String("db", "/var/lib/serial-proxy/history.db", "SQLite history path (empty=disabled)")
	port := flag.Int("port", 8800, "HTTP port (REST mode)")
	bind := flag.String("bind", "127.0.0.1", "Bind address (REST mode)")
	flag.Var(&vmids, "vmid", "VMID(s) to pre-connect (repeatable)")
	flag.Parse()

	// Structured JSON logging.
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
		debugMode.Store(true)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	if len(vmids) == 0 {
		vmids = vmidList{700}
	}

	// Connection pool.
	pool = serial.NewPool(*debug)
	defer pool.Close()

	for _, vmid := range vmids {
		conn, err := pool.Get(vmid)
		if err != nil {
			slog.Warn("pre_connect_failed", "vmid", vmid, "error", err.Error())
			continue
		}
		slog.Info("pre_connected", "vmid", vmid, "socket", conn.SockPath)
	}

	// History database.
	if *dbPath != "" {
		if dir := *dbPath; dir != "" {
			dirPath := dir[:max(0, strings.LastIndex(dir, "/"))]
			if dirPath != "" {
				os.MkdirAll(dirPath, 0o750)
			}
		}
		var err error
		histDB, err = history.Open(*dbPath)
		if err != nil {
			slog.Error("history_db_error", "path", *dbPath, "error", err.Error())
		} else {
			defer histDB.Close()
		}
	}

	if *rest {
		mux := http.NewServeMux()
		mux.HandleFunc("/", restHelp)
		mux.HandleFunc("/health", restHealth)
		mux.HandleFunc("/status", restStatus)
		mux.HandleFunc("/exec", restExecRouter)
		mux.HandleFunc("/enable", restEnable)
		mux.HandleFunc("/connect", restConnect)
		mux.HandleFunc("/disconnect", restDisconnect)
		mux.HandleFunc("/history", restHistory)
		mux.HandleFunc("/stats", restStats)

		addr := fmt.Sprintf("%s:%d", *bind, *port)
		slog.Info("rest_server_start", "addr", addr, "version", version, "debug", debugMode.Load())
		for _, s := range serial.AvailableSockets() {
			slog.Info("available_socket", "vmid", s)
		}
		srv := &http.Server{Addr: addr, Handler: mux}
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("server_error", "error", err.Error())
			os.Exit(1)
		}
		return
	}

	// MCP stdio mode.
	server := mcp.NewServer(&mcp.Implementation{Name: "serial-proxy", Version: "v" + version}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "serial_exec", Description: "Execute CLI command on a QEMU VM serial console"}, mcpExec)
	mcp.AddTool(server, &mcp.Tool{Name: "serial_status", Description: "Show connection status and available sockets"}, mcpStatus)
	mcp.AddTool(server, &mcp.Tool{Name: "serial_enable", Description: "Enter privileged/enable mode with password"}, mcpEnable)
	mcp.AddTool(server, &mcp.Tool{Name: "serial_raw", Description: "Send raw data to serial console"}, mcpRaw)
	slog.Info("mcp_server_start", "version", version, "tools", 4)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		slog.Error("mcp_error", "error", err.Error())
		os.Exit(1)
	}
}
