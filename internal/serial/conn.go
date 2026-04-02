// Package serial provides persistent connections to QEMU serial UNIX sockets.
//
// Each [Conn] manages a single UNIX socket with automatic reconnect,
// command echo/prompt stripping, and ANSI escape removal. All methods
// are safe for concurrent use — a mutex serializes access to the
// underlying socket so multiple agents can share a single VM connection.
package serial

import (
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	dialTimeout  = 5 * time.Second
	drainDefault = 200 * time.Millisecond
	readBuf      = 65536
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// Conn manages a persistent connection to a QEMU serial UNIX socket.
type Conn struct {
	VMID     int
	SockPath string
	conn     net.Conn
	mu       sync.Mutex
	debug    bool
}

// New dials the serial socket for the given VMID.
func New(vmid int, debug bool) (*Conn, error) {
	sc := &Conn{
		VMID:     vmid,
		SockPath: fmt.Sprintf("/var/run/qemu-server/%d.serial0", vmid),
		debug:    debug,
	}
	if err := sc.reconnect(); err != nil {
		return nil, err
	}
	return sc, nil
}

func (c *Conn) reconnect() error {
	if c.conn != nil {
		c.conn.Close()
	}
	conn, err := net.DialTimeout("unix", c.SockPath, dialTimeout)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", c.SockPath, err)
	}
	c.conn = conn
	c.drain(300 * time.Millisecond)
	return nil
}

func (c *Conn) drain(timeout time.Duration) []byte {
	var data []byte
	buf := make([]byte, readBuf)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 10*time.Millisecond {
			break
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(remaining)); err != nil {
			break
		}
		n, err := c.conn.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return data
}

func (c *Conn) write(data []byte) error {
	if _, err := c.conn.Write(data); err != nil {
		if reconnErr := c.reconnect(); reconnErr != nil {
			return fmt.Errorf("reconnect failed: %w", reconnErr)
		}
		if _, err := c.conn.Write(data); err != nil {
			return fmt.Errorf("write after reconnect: %w", err)
		}
	}
	return nil
}

// Exec sends a CLI command and waits for the prompt, returning clean output.
func (c *Conn) Exec(cmd string, wait time.Duration, prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.drain(drainDefault)
	if err := c.write([]byte(cmd + "\r")); err != nil {
		return "", err
	}

	time.Sleep(300 * time.Millisecond)

	var output []byte
	buf := make([]byte, readBuf)
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 10*time.Millisecond {
			break
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(min(remaining, 500*time.Millisecond))); err != nil {
			break
		}
		n, err := c.conn.Read(buf)
		if n > 0 {
			output = append(output, buf[:n]...)
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 && strings.Contains(lines[len(lines)-1], prompt) {
				break
			}
		}
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			break
		}
	}

	if c.debug {
		slog.Debug("raw_socket_output", "vmid", c.VMID, "bytes", len(output))
	}

	return CleanOutput(string(output), cmd, prompt), nil
}

// Raw sends raw data and returns the raw (ANSI-cleaned) response.
func (c *Conn) Raw(data string, wait time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.drain(drainDefault)
	if err := c.write([]byte(data)); err != nil {
		return "", err
	}
	time.Sleep(100 * time.Millisecond)
	result := c.drain(wait)
	return CleanANSI(string(result)), nil
}

// Enable enters privileged mode with optional password.
func (c *Conn) Enable(password string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.drain(drainDefault)
	if err := c.write([]byte("enable\r")); err != nil {
		return "", fmt.Errorf("write enable: %w", err)
	}
	time.Sleep(500 * time.Millisecond)
	out := c.drain(2 * time.Second)

	if strings.Contains(string(out), "Password:") && password != "" {
		if err := c.write([]byte(password + "\r")); err != nil {
			return "", fmt.Errorf("write password: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
		out = append(out, c.drain(2*time.Second)...)
	}

	if err := c.write([]byte("terminal pager 0\r")); err != nil {
		return "", fmt.Errorf("write terminal pager: %w", err)
	}
	time.Sleep(300 * time.Millisecond)
	out = append(out, c.drain(1*time.Second)...)

	return CleanANSI(string(out)), nil
}

// IsConnected returns true if the underlying socket is open.
func (c *Conn) IsConnected() bool { return c.conn != nil }

// Close shuts down the connection.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// CleanOutput strips command echo, trailing prompt, ANSI codes, and \r.
func CleanOutput(raw, cmd, prompt string) string {
	text := CleanANSI(raw)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.Contains(lines[0], strings.TrimSpace(cmd)) {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.Contains(lines[len(lines)-1], prompt) {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// CleanANSI removes ANSI escape sequences.
func CleanANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
