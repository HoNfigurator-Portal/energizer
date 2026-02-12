package connector

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/protocol"
)

const (
	chatKeepAliveInterval = 15 * time.Second
	chatReconnectDelay    = 10 * time.Second
	chatConnectTimeout    = 30 * time.Second
)

// ChatServerConnector manages the persistent TCP connection to the HoN chat server.
// It handles the binary handshake protocol, keepalive heartbeats, and processes
// incoming requests such as replay download requests from players.
type ChatServerConnector struct {
	mu sync.Mutex

	cfg      *config.Config
	eventBus *events.EventBus
	parser   *protocol.ChatServerParser

	conn      net.Conn
	connected bool

	// Shutdown signal
	stopCh chan struct{}
}

// NewChatServerConnector creates a new chat server connector.
func NewChatServerConnector(cfg *config.Config, eventBus *events.EventBus) *ChatServerConnector {
	return &ChatServerConnector{
		cfg:      cfg,
		eventBus: eventBus,
		parser:   protocol.NewChatServerParser(),
		stopCh:   make(chan struct{}),
	}
}

// ManageConnection maintains the TCP connection to the chat server.
// It requires the master server connector to be authenticated first
// to obtain the chat server address and session cookie.
func (c *ChatServerConnector) ManageConnection(ctx context.Context, masterConn *MasterServerConnector) error {
	log.Info().Msg("starting chat server connection manager")

	for {
		select {
		case <-ctx.Done():
			c.disconnect()
			return nil
		default:
		}

		// Wait for master server authentication
		if !masterConn.IsAuthenticated() {
			log.Debug().Msg("waiting for master server authentication...")
			time.Sleep(2 * time.Second)
			continue
		}

		// Connect to chat server
		chatIP, chatPort := masterConn.GetChatServerAddr()
		if chatIP == "" || chatPort == 0 {
			log.Warn().Msg("chat server address not available, waiting...")
			time.Sleep(5 * time.Second)
			continue
		}

		if err := c.connect(ctx, chatIP, chatPort, masterConn); err != nil {
			log.Error().Err(err).Msg("chat server connection failed")
			time.Sleep(chatReconnectDelay)
			continue
		}

		// Enter the read loop (blocks until disconnected or error)
		c.readLoop(ctx)

		// If we get here, we were disconnected - try to reconnect
		log.Warn().Msg("disconnected from chat server, reconnecting...")
		time.Sleep(chatReconnectDelay)
	}
}

// connect establishes a TCP connection to the chat server and performs handshake.
func (c *ChatServerConnector) connect(ctx context.Context, ip string, port int, masterConn *MasterServerConnector) error {
	addr := fmt.Sprintf("%s:%d", ip, port)
	log.Info().Str("addr", addr).Msg("connecting to chat server")

	dialer := net.Dialer{Timeout: chatConnectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to chat server at %s: %w", addr, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	// Perform handshake
	if err := c.handshake(masterConn); err != nil {
		c.disconnect()
		return fmt.Errorf("chat server handshake failed: %w", err)
	}

	// Send server info
	if err := c.sendServerInfo(); err != nil {
		c.disconnect()
		return fmt.Errorf("failed to send server info: %w", err)
	}

	log.Info().Str("addr", addr).Msg("connected to chat server")

	// Start keepalive goroutine
	go c.keepAlive(ctx)

	return nil
}

// handshake sends the initial handshake packet (0x1600).
func (c *ChatServerConnector) handshake(masterConn *MasterServerConnector) error {
	sessionCookie := masterConn.GetSessionCookie()
	serverID := masterConn.GetServerID()

	payload := protocol.BuildChatHandshake(sessionCookie, serverID)

	c.mu.Lock()
	defer c.mu.Unlock()

	return protocol.WriteChatPacket(c.conn, protocol.PktChatHandshake, payload)
}

// sendServerInfo sends the server information packet (0x1602).
func (c *ChatServerConnector) sendServerInfo() error {
	honData := c.cfg.GetHoNData()

	payload := protocol.BuildChatServerInfo(
		honData.Region,
		"", // IP will be auto-detected by chat server
		honData.Name,
		honData.ServerVersion,
	)

	c.mu.Lock()
	defer c.mu.Unlock()

	return protocol.WriteChatPacket(c.conn, protocol.PktChatServerInfo, payload)
}

// keepAlive sends periodic heartbeat packets to maintain the connection.
func (c *ChatServerConnector) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(chatKeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.mu.Lock()
			if !c.connected || c.conn == nil {
				c.mu.Unlock()
				return
			}
			err := protocol.WriteChatPacket(c.conn, protocol.PktChatKeepAlive, nil)
			c.mu.Unlock()

			if err != nil {
				log.Warn().Err(err).Msg("failed to send chat keepalive")
				return
			}

			log.Trace().Msg("chat keepalive sent")
		}
	}
}

// readLoop continuously reads and processes packets from the chat server.
func (c *ChatServerConnector) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		connected := c.connected
		c.mu.Unlock()

		if !connected || conn == nil {
			return
		}

		// Set read deadline
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		pkt, err := protocol.ReadChatPacket(conn)
		if err != nil {
			if err == io.EOF {
				log.Info().Msg("chat server closed connection")
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is OK, just continue
				continue
			} else {
				log.Error().Err(err).Msg("error reading from chat server")
			}
			c.disconnect()
			return
		}

		// Parse the packet
		result, err := c.parser.ParseChatPacket(pkt)
		if err != nil {
			log.Warn().Err(err).Uint16("cmd", pkt.Command).Msg("failed to parse chat packet")
			continue
		}

		// Handle specific packet types
		c.handlePacket(ctx, pkt.Command, result)
	}
}

// handlePacket processes parsed chat server packets.
func (c *ChatServerConnector) handlePacket(ctx context.Context, cmd uint16, data interface{}) {
	switch cmd {
	case protocol.PktChatShutdown:
		if shutdown, ok := data.(*protocol.ChatShutdown); ok {
			log.Warn().Str("reason", shutdown.Reason).Msg("chat server sent shutdown notice")
			c.disconnect()
		}

	case protocol.PktChatReplayReq:
		if req, ok := data.(*protocol.ChatReplayRequest); ok {
			log.Info().
				Uint32("match_id", req.MatchID).
				Uint32("account_id", req.AccountID).
				Msg("replay request from chat server")

			c.eventBus.Emit(ctx, events.Event{
				Type:   events.EventHandleReplayRequest,
				Source: "chat_server",
				Payload: map[string]uint32{
					"match_id":   req.MatchID,
					"account_id": req.AccountID,
				},
			})
		}

	case protocol.PktChatKeepAlive:
		log.Trace().Msg("chat keepalive response received")
	}
}

// SendReplayStatus sends a replay status update to the chat server.
func (c *ChatServerConnector) SendReplayStatus(matchID uint32, status byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected to chat server")
	}

	payload := protocol.BuildChatReplayStatus(matchID, status)
	return protocol.WriteChatPacket(c.conn, protocol.PktChatReplayStatus, payload)
}

// disconnect closes the chat server connection.
func (c *ChatServerConnector) disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	log.Info().Msg("disconnected from chat server")
}

// IsConnected returns whether connected to the chat server.
func (c *ChatServerConnector) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}
