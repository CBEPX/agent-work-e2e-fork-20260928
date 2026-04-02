package serial

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Pool manages a set of serial connections indexed by VMID.
// Safe for concurrent use by multiple agents.
type Pool struct {
	mu    sync.Mutex
	conns map[int]*Conn
	debug bool
}

// NewPool creates an empty connection pool.
func NewPool(debug bool) *Pool {
	return &Pool{conns: make(map[int]*Conn), debug: debug}
}

// Get returns an existing connection or creates a new one.
func (p *Pool) Get(vmid int) (*Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[vmid]; ok {
		return c, nil
	}
	c, err := New(vmid, p.debug)
	if err != nil {
		return nil, fmt.Errorf("pool connect vmid %d: %w", vmid, err)
	}
	p.conns[vmid] = c
	return c, nil
}

// Remove disconnects and removes a connection.
func (p *Pool) Remove(vmid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[vmid]; ok {
		c.Close()
		delete(p.conns, vmid)
	}
}

// ConnInfo describes a single connection's state.
type ConnInfo struct {
	Connected bool   `json:"connected"`
	Socket    string `json:"socket"`
}

// Status returns the state of all connections.
func (p *Pool) Status() map[string]ConnInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := make(map[string]ConnInfo, len(p.conns))
	for vmid, c := range p.conns {
		m[fmt.Sprintf("%d", vmid)] = ConnInfo{
			Connected: c.IsConnected(),
			Socket:    c.SockPath,
		}
	}
	return m
}

// DefaultVMID returns the first connected VMID, or 700.
func (p *Pool) DefaultVMID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for vmid := range p.conns {
		return vmid
	}
	return 700
}

// Close shuts down all connections.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = make(map[int]*Conn)
}

// AvailableSockets lists all .serial0 sockets in /var/run/qemu-server/.
func AvailableSockets() []string {
	entries, _ := os.ReadDir("/var/run/qemu-server/")
	var result []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".serial0") {
			result = append(result, strings.TrimSuffix(e.Name(), ".serial0"))
		}
	}
	return result
}
