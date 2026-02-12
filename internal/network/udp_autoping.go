package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/protocol"
)

// UDPAutoPingListener responds to game client auto-ping probes.
// Game clients send a UDP packet with magic byte 0xCA to discover
// available servers. The listener responds with server name and version.
//
// The listener runs on port (starting_game_port - 1) or (+10000 if proxy enabled).
type UDPAutoPingListener struct {
	cfg    *config.Config
	conn   *net.UDPConn
}

// NewUDPAutoPingListener creates a new UDP auto-ping listener.
func NewUDPAutoPingListener(cfg *config.Config) *UDPAutoPingListener {
	return &UDPAutoPingListener{
		cfg: cfg,
	}
}

// Start begins listening for UDP auto-ping probes.
func (l *UDPAutoPingListener) Start(ctx context.Context) error {
	port := l.calculatePort()
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	}

	// Use SO_REUSEADDR to allow immediate rebinding after restart
	lc := ReuseAddrListenConfig()
	pc, err := lc.ListenPacket(ctx, "udp4", addr.String())
	if err != nil {
		return fmt.Errorf("failed to start UDP AutoPing listener on port %d: %w", port, err)
	}
	l.conn = pc.(*net.UDPConn)

	log.Info().Int("port", port).Msg("UDP AutoPing listener started")

	// Close when context is cancelled
	go func() {
		<-ctx.Done()
		l.conn.Close()
	}()

	// Read and respond to ping probes
	buf := make([]byte, 1024)
	for {
		n, remoteAddr, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				log.Info().Msg("UDP AutoPing listener stopping")
				return nil
			default:
				log.Error().Err(err).Msg("UDP read error")
				continue
			}
		}

		if n < 1 {
			continue
		}

		// Check for magic byte
		if buf[0] != protocol.AutoPingMagicByte {
			continue
		}

		// Build and send response
		response := protocol.BuildAutoPingResponse(
			l.cfg.HoNData.Name,
			l.cfg.HoNData.ServerVersion,
		)

		if _, err := l.conn.WriteToUDP(response, remoteAddr); err != nil {
			log.Warn().
				Err(err).
				Str("remote", remoteAddr.String()).
				Msg("failed to send AutoPing response")
		}

		log.Trace().
			Str("remote", remoteAddr.String()).
			Msg("responded to AutoPing probe")
	}
}

// calculatePort determines the listening port for auto-ping.
func (l *UDPAutoPingListener) calculatePort() int {
	port := l.cfg.HoNData.StartingGamePort - 1
	if l.cfg.HoNData.EnableProxy {
		port += 10000
	}
	return port
}

// SelfTest sends a test ping to verify the listener is working.
func (l *UDPAutoPingListener) SelfTest() error {
	port := l.calculatePort()
	addr := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: port,
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("self-test dial failed: %w", err)
	}
	defer conn.Close()

	// Send magic byte
	if _, err := conn.Write([]byte{protocol.AutoPingMagicByte}); err != nil {
		return fmt.Errorf("self-test write failed: %w", err)
	}

	// Read response with timeout
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(buf); err != nil {
		return fmt.Errorf("self-test read failed: %w", err)
	}

	log.Debug().Int("port", port).Msg("AutoPing self-test passed")
	return nil
}

// Stop closes the UDP listener.
func (l *UDPAutoPingListener) Stop() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}
