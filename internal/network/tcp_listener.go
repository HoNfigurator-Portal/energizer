package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/protocol"
)

const (
	// ReadTimeout is how long to wait for data before considering a connection stale.
	ReadTimeout = 60 * time.Second
)

// ServerManagerInterface defines the interface for the server manager
// used by the TCP listener to register connections.
type ServerManagerInterface interface {
	GetConnectionRegistry() *ConnectionRegistry
	HandleServerEvent(ctx context.Context, event *events.Event)
}

// TCPListener listens for incoming TCP connections from game server instances.
// Game servers connect to 127.0.0.1:{managerPort} (default 1134) and
// communicate using a binary protocol with length-prefixed packets.
type TCPListener struct {
	cfg      *config.Config
	eventBus *events.EventBus
	manager  ServerManagerInterface
	parser   *protocol.GameManagerParser
	listener net.Listener
}

// NewTCPListener creates a new TCP listener.
func NewTCPListener(cfg *config.Config, eventBus *events.EventBus, manager ServerManagerInterface) *TCPListener {
	return &TCPListener{
		cfg:      cfg,
		eventBus: eventBus,
		manager:  manager,
		parser:   protocol.NewGameManagerParser(),
	}
}

// Start begins listening for game server TCP connections.
// This is one of the 5 main concurrent tasks from the original Python implementation.
func (l *TCPListener) Start(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", l.cfg.HoNData.ManagerPort)

	// Use SO_REUSEADDR to allow immediate rebinding after restart
	lc := ReuseAddrListenConfig()
	var err error
	l.listener, err = lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener on %s: %w", addr, err)
	}

	log.Info().Str("addr", addr).Msg("TCP listener started")

	// Accept connections in a loop
	go func() {
		<-ctx.Done()
		l.listener.Close()
	}()

	for {
		conn, err := l.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Info().Msg("TCP listener stopping")
				return nil
			default:
				log.Error().Err(err).Msg("failed to accept connection")
				continue
			}
		}

		log.Debug().
			Str("remote", conn.RemoteAddr().String()).
			Msg("new game server connection")

		// Handle each connection in its own goroutine
		go l.handleConnection(ctx, conn)
	}
}

// handleConnection processes a single game server TCP connection.
// It performs the initial handshake (0x40 server announce) to identify
// which game server port this connection belongs to, then enters the
// packet processing loop.
func (l *TCPListener) handleConnection(ctx context.Context, rawConn net.Conn) {
	conn := NewConnection(rawConn)
	defer conn.Close()

	logger := log.With().
		Str("component", "tcp_handler").
		Str("remote", rawConn.RemoteAddr().String()).
		Logger()

	// Wait for the first packet (should be server announce 0x40)
	data, err := conn.ReadPacket(30 * time.Second)
	if err != nil {
		logger.Error().Err(err).Msg("failed to read handshake packet")
		return
	}

	// Parse handshake
	event, err := l.parser.Parse(data)
	if err != nil {
		logger.Error().Err(err).Msg("failed to parse handshake packet")
		return
	}

	if event.Type != events.EventServerAnnounce {
		logger.Error().
			Str("event_type", string(event.Type)).
			Msg("expected server announce as first packet")
		return
	}

	// Extract port from announce payload
	announce, ok := event.Payload.(events.ServerAnnouncePayload)
	if !ok {
		logger.Error().Msg("invalid server announce payload")
		return
	}

	port := announce.Port
	conn.SetPort(port)

	logger = log.With().
		Str("component", "tcp_handler").
		Uint16("port", port).
		Logger()

	logger.Info().Msg("game server identified, registering connection")

	// Register connection with the server manager
	registry := l.manager.GetConnectionRegistry()
	registry.Register(port, conn)
	defer registry.Unregister(port)

	// Emit the announce event
	l.eventBus.Emit(ctx, *event)

	// Enter packet processing loop
	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("context cancelled, closing connection")
			return
		default:
		}

		data, err := conn.ReadPacket(ReadTimeout)
		if err != nil {
			if conn.IsClosed() {
				return
			}
			// Check if it's a timeout
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Warn().Msg("connection timed out (no data for 60s), killing server")
				// Emit server closed event
				l.eventBus.Emit(ctx, events.Event{
					Type:   events.EventServerClosed,
					Source: fmt.Sprintf("game_server:%d", port),
					Payload: events.ServerAnnouncePayload{Port: port},
				})
				return
			}
			logger.Error().Err(err).Msg("read error, closing connection")
			return
		}

		// Parse the packet
		event, err := l.parser.Parse(data)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to parse packet")
			continue
		}

		if event != nil {
			// Dispatch event to event bus (async)
			l.eventBus.Emit(ctx, *event)

			// Also directly notify the server manager for immediate processing
			l.manager.HandleServerEvent(ctx, event)
		}
	}
}

// Stop gracefully stops the TCP listener.
func (l *TCPListener) Stop() error {
	if l.listener != nil {
		return l.listener.Close()
	}
	return nil
}
