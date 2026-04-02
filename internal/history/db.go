// Package history provides SQLite-backed command execution history.
//
// All writes are serialized by a mutex (safe for concurrent goroutines).
// Reads use WAL mode for non-blocking concurrent access.
package history

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DB stores command execution history in SQLite.
type DB struct {
	db *sql.DB
	mu sync.Mutex
}

// Open creates or opens the SQLite database at path.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	slog.Info("history_db_opened", "path", path)
	return &DB{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS commands (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		ts         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
		rid        TEXT    NOT NULL,
		agent      TEXT    NOT NULL DEFAULT 'anonymous',
		vmid       INTEGER NOT NULL,
		cmd        TEXT    NOT NULL,
		output     TEXT    NOT NULL DEFAULT '',
		duration_s REAL    NOT NULL DEFAULT 0,
		error      TEXT    NOT NULL DEFAULT '',
		method     TEXT    NOT NULL DEFAULT 'rest'
	);
	CREATE INDEX IF NOT EXISTS idx_commands_agent ON commands(agent);
	CREATE INDEX IF NOT EXISTS idx_commands_vmid  ON commands(vmid);
	CREATE INDEX IF NOT EXISTS idx_commands_ts    ON commands(ts);
	`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Record inserts a command execution into history.
func (h *DB) Record(rid, agent string, vmid int, cmd, output, errMsg, method string, dur time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.db.Exec(
		`INSERT INTO commands (rid, agent, vmid, cmd, output, duration_s, error, method)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rid, agent, vmid, cmd, output, dur.Seconds(), errMsg, method,
	)
	if err != nil {
		slog.Error("history_write_error", "error", err.Error())
	}
}

// Entry is a single command execution record.
type Entry struct {
	ID        int     `json:"id"`
	Timestamp string  `json:"ts"`
	RID       string  `json:"rid"`
	Agent     string  `json:"agent"`
	VMID      int     `json:"vmid"`
	Cmd       string  `json:"cmd"`
	Output    string  `json:"output"`
	DurationS float64 `json:"duration_s"`
	Error     string  `json:"error,omitempty"`
	Method    string  `json:"method"`
}

// Query returns recent history entries, optionally filtered.
func (h *DB) Query(agent string, vmid, limit int) ([]Entry, error) {
	q := `SELECT id, ts, rid, agent, vmid, cmd, output, duration_s, error, method FROM commands WHERE 1=1`
	var args []any
	if agent != "" {
		q += " AND agent = ?"
		args = append(args, agent)
	}
	if vmid != 0 {
		q += " AND vmid = ?"
		args = append(args, vmid)
	}
	q += " ORDER BY id DESC LIMIT ?"
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)

	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.RID, &e.Agent, &e.VMID,
			&e.Cmd, &e.Output, &e.DurationS, &e.Error, &e.Method); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AgentStat is per-agent aggregate statistics.
type AgentStat struct {
	Agent   string  `json:"agent"`
	Count   int     `json:"count"`
	AvgDurS float64 `json:"avg_duration_s"`
}

// Stats returns aggregate statistics.
func (h *DB) Stats() (total int, agents []AgentStat, err error) {
	if err := h.db.QueryRow("SELECT COUNT(*) FROM commands").Scan(&total); err != nil {
		return 0, nil, fmt.Errorf("count: %w", err)
	}
	rows, err := h.db.Query(`
		SELECT agent, COUNT(*) as cnt, ROUND(AVG(duration_s), 3) as avg_dur
		FROM commands GROUP BY agent ORDER BY cnt DESC LIMIT 20
	`)
	if err != nil {
		return total, nil, fmt.Errorf("agent stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a AgentStat
		if scanErr := rows.Scan(&a.Agent, &a.Count, &a.AvgDurS); scanErr != nil {
			continue
		}
		agents = append(agents, a)
	}
	return total, agents, rows.Err()
}

// Close shuts down the database.
func (h *DB) Close() {
	if h.db != nil {
		h.db.Close()
	}
}
