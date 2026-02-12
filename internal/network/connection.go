// Package network implements the network listeners and connection handlers
// for game server TCP communication and UDP auto-ping.
package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/protocol"
)

// Connection wraps a TCP connection to a game server instance.
// Each game server maintains one persistent TCP connection to the manager
// on 127.0.0.1:1134 for binary protocol communication.
type Connection struct {
	mu     sync.Mutex
	conn   net.Conn
	port   uint16
	logger zerolog.Logger

	// Timestamps
	connectedAt  time.Time
	lastActivity time.Time

	// State
	closed bool
}

// NewConnection wraps an existing net.Conn.
func NewConnection(conn net.Conn) *Connection {
	now := time.Now()
	return &Connection{
		conn:         conn,
		connectedAt:  now,
		lastActivity: now,
		logger:       log.With().Str("component", "connection").Str("remote", conn.RemoteAddr().String()).Logger(),
	}
}

// SetPort associates this connection with a game server port.
func (c *Connection) SetPort(port uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.port = port
	c.logger = log.With().
		Str("component", "connection").
		Uint16("port", port).
		Logger()
}

// Port returns the associated game server port.
func (c *Connection) Port() uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port
}

// ReadPacket reads a single binary packet from the connection.
// Blocks until a packet is available or timeout occurs.
func (c *Connection) ReadPacket(timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		c.conn.SetReadDeadline(time.Now().Add(timeout))
	}

	data, err := protocol.ReadPacket(c.conn)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	return data, nil
}

// WritePacket sends a binary packet through the connection.
func (c *Connection) WritePacket(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("connection is closed")
	}

	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := protocol.WritePacket(c.conn, data)
	if err != nil {
		return fmt.Errorf("failed to write packet: %w", err)
	}

	c.lastActivity = time.Now()
	return nil
}

// SendCommand sends a command to the game server.
func (c *Connection) SendCommand(command string) error {
	data := protocol.BuildManagerCommand(command)
	return c.WritePacket(data)
}

// SendMessage sends an in-game message through the game server.
func (c *Connection) SendMessage(message string) error {
	data := protocol.BuildManagerMessage(message)
	return c.WritePacket(data)
}

// KickPlayer sends a kick command for a specific player.
func (c *Connection) KickPlayer(playerID uint32, reason string) error {
	data := protocol.BuildManagerKick(playerID, reason)
	return c.WritePacket(data)
}

// Close closes the connection.
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.logger.Info().Msg("connection closed")
	return c.conn.Close()
}

// IsClosed returns whether the connection has been closed.
func (c *Connection) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// LastActivity returns the time of the last read/write activity.
func (c *Connection) LastActivity() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastActivity
}

// ConnectedAt returns the time the connection was established.
func (c *Connection) ConnectedAt() time.Time {
	return c.connectedAt
}

// RemoteAddr returns the remote address of the connection.
func (c *Connection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// ConnectionRegistry tracks active game server connections.
type ConnectionRegistry struct {
	mu    sync.RWMutex
	conns map[uint16]*Connection // port -> connection
}

// NewConnectionRegistry creates a new ConnectionRegistry.
func NewConnectionRegistry() *ConnectionRegistry {
	return &ConnectionRegistry{
		conns: make(map[uint16]*Connection),
	}
}

// Register adds a connection to the registry.
func (r *ConnectionRegistry) Register(port uint16, conn *Connection) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close existing connection if any
	if existing, ok := r.conns[port]; ok {
		existing.Close()
	}

	r.conns[port] = conn
	log.Debug().Uint16("port", port).Msg("connection registered")
}

// Unregister removes a connection from the registry.
func (r *ConnectionRegistry) Unregister(port uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conn, ok := r.conns[port]; ok {
		conn.Close()
		delete(r.conns, port)
		log.Debug().Uint16("port", port).Msg("connection unregistered")
	}
}

// Get returns the connection for a specific port.
func (r *ConnectionRegistry) Get(port uint16) (*Connection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	conn, ok := r.conns[port]
	return conn, ok
}

// GetAll returns all active connections.
func (r *ConnectionRegistry) GetAll() map[uint16]*Connection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[uint16]*Connection, len(r.conns))
	for k, v := range r.conns {
		result[k] = v
	}
	return result
}

// Count returns the number of active connections.
func (r *ConnectionRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.conns)
}

// CloseAll closes all connections in the registry.
func (r *ConnectionRegistry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for port, conn := range r.conns {
		conn.Close()
		delete(r.conns, port)
	}

	log.Info().Msg("all connections closed")
}

// CleanStale closes connections that have been inactive for longer than timeout.
func (r *ConnectionRegistry) CleanStale(timeout time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	cleaned := 0
	cutoff := time.Now().Add(-timeout)

	for port, conn := range r.conns {
		if conn.LastActivity().Before(cutoff) {
			conn.Close()
			delete(r.conns, port)
			cleaned++
			log.Warn().
				Uint16("port", port).
				Time("last_activity", conn.LastActivity()).
				Msg("cleaned stale connection")
		}
	}

	return cleaned
}

// SendToAll sends a packet to all connected game servers.
func (r *ConnectionRegistry) SendToAll(ctx context.Context, data []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for port, conn := range r.conns {
		if err := conn.WritePacket(data); err != nil {
			log.Warn().Err(err).Uint16("port", port).Msg("failed to send to server")
		}
	}
}
