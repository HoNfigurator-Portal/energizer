// Package network contains network-related components for Energizer.
//
// GameProxy implements a TCP/UDP reverse proxy for game server instances.
// When man_enableProxy is enabled, the game server registers proxy ports with
// the master server. Clients connect to these proxy ports; the proxy forwards
// all traffic to 127.0.0.1:<game_port>. This hides the real game ports from
// the internet, providing DDoS protection.
package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Default rate limits for DDoS protection.
const (
	DefaultMaxTCPConnPerSec  = 10  // max new TCP connections per second per source IP
	DefaultMaxUDPPktPerSec   = 300 // max UDP packets per second per source IP
	DefaultMaxConcurrentConn = 100 // max concurrent TCP connections per proxy port
	udpBufSize               = 4096
	udpSessionTimeout        = 60 * time.Second
)

// GameProxyConfig holds the configuration for a game proxy instance.
type GameProxyConfig struct {
	GamePort        uint16 // local game server port (e.g. 11235)
	ProxyPort       uint16 // public-facing proxy port (e.g. 11297)
	VoiceLocalPort  uint16 // local voice port (e.g. 11335)
	VoiceRemotePort uint16 // public-facing voice port (e.g. 11897)
	ServerID        int    // for logging
}

// GameProxy is a per-instance TCP/UDP reverse proxy with rate limiting.
type GameProxy struct {
	cfg    GameProxyConfig
	logger zerolog.Logger

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopped  atomic.Bool

	// listeners to close on stop
	tcpListener net.Listener
	udpConn     *net.UDPConn
	voiceConn   *net.UDPConn
}

// NewGameProxy creates a new game proxy.
func NewGameProxy(cfg GameProxyConfig) *GameProxy {
	return &GameProxy{
		cfg: cfg,
		logger: log.With().
			Int("server_id", cfg.ServerID).
			Uint16("proxy_port", cfg.ProxyPort).
			Uint16("game_port", cfg.GamePort).
			Logger(),
	}
}

// Start launches all proxy listeners (TCP game, UDP game, UDP voice).
func (gp *GameProxy) Start(ctx context.Context) error {
	ctx, gp.cancel = context.WithCancel(ctx)

	// TCP proxy: proxyPort -> 127.0.0.1:gamePort
	if err := gp.startTCPProxy(ctx); err != nil {
		return fmt.Errorf("TCP proxy failed: %w", err)
	}

	// UDP proxy: proxyPort -> 127.0.0.1:gamePort
	if err := gp.startUDPProxy(ctx, gp.cfg.ProxyPort, gp.cfg.GamePort, "game"); err != nil {
		return fmt.Errorf("UDP game proxy failed: %w", err)
	}

	// Voice UDP proxy: voiceRemotePort -> 127.0.0.1:voiceLocalPort
	if err := gp.startUDPProxy(ctx, gp.cfg.VoiceRemotePort, gp.cfg.VoiceLocalPort, "voice"); err != nil {
		return fmt.Errorf("UDP voice proxy failed: %w", err)
	}

	gp.logger.Info().
		Uint16("tcp_proxy", gp.cfg.ProxyPort).
		Uint16("udp_proxy", gp.cfg.ProxyPort).
		Uint16("voice_proxy", gp.cfg.VoiceRemotePort).
		Msg("game proxy started")

	return nil
}

// Stop shuts down all proxy listeners and waits for goroutines to finish.
func (gp *GameProxy) Stop() {
	if gp.stopped.Swap(true) {
		return // already stopped
	}
	gp.logger.Info().Msg("stopping game proxy")

	if gp.cancel != nil {
		gp.cancel()
	}

	// Close listeners to unblock Accept/ReadFrom
	if gp.tcpListener != nil {
		gp.tcpListener.Close()
	}
	if gp.udpConn != nil {
		gp.udpConn.Close()
	}
	if gp.voiceConn != nil {
		gp.voiceConn.Close()
	}

	gp.wg.Wait()
	gp.logger.Info().Msg("game proxy stopped")
}

// IsRunning returns true if the proxy has not been stopped.
func (gp *GameProxy) IsRunning() bool {
	return !gp.stopped.Load()
}

// ---- TCP Proxy ----

func (gp *GameProxy) startTCPProxy(ctx context.Context) error {
	listenAddr := fmt.Sprintf(":%d", gp.cfg.ProxyPort)
	lc := ReuseAddrListenConfig()
	ln, err := lc.Listen(ctx, "tcp", listenAddr)
	if err != nil {
		return err
	}
	gp.tcpListener = ln

	rateLimiter := newRateTracker(DefaultMaxTCPConnPerSec)
	var connCount atomic.Int32

	gp.wg.Add(1)
	go func() {
		defer gp.wg.Done()
		defer ln.Close()

		for {
			conn, err := ln.Accept()
			if err != nil {
				if gp.stopped.Load() || ctx.Err() != nil {
					return
				}
				gp.logger.Debug().Err(err).Msg("TCP accept error")
				continue
			}

			srcIP := extractIP(conn.RemoteAddr())

			// Rate limit: max connections per second per source IP
			if !rateLimiter.allow(srcIP) {
				gp.logger.Warn().Str("src", srcIP).Msg("TCP rate limit exceeded, dropping connection")
				conn.Close()
				continue
			}

			// Max concurrent connections
			if int(connCount.Load()) >= DefaultMaxConcurrentConn {
				gp.logger.Warn().Str("src", srcIP).Msg("TCP max concurrent connections reached, dropping")
				conn.Close()
				continue
			}

			connCount.Add(1)
			gp.wg.Add(1)
			go func() {
				defer gp.wg.Done()
				defer connCount.Add(-1)
				gp.handleTCPConn(ctx, conn)
			}()
		}
	}()

	return nil
}

func (gp *GameProxy) handleTCPConn(ctx context.Context, clientConn net.Conn) {
	defer clientConn.Close()

	target := fmt.Sprintf("127.0.0.1:%d", gp.cfg.GamePort)
	serverConn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		gp.logger.Debug().Err(err).Msg("failed to connect to game server")
		return
	}
	defer serverConn.Close()

	// Bidirectional copy with context awareness
	done := make(chan struct{})

	go func() {
		io.Copy(serverConn, clientConn)
		close(done)
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
	// The other direction
	io.Copy(clientConn, serverConn)
}

// ---- UDP Proxy ----

func (gp *GameProxy) startUDPProxy(ctx context.Context, listenPort, targetPort uint16, label string) error {
	listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: int(listenPort)}

	lc := ReuseAddrListenConfig()
	pc, err := lc.ListenPacket(ctx, "udp4", listenAddr.String())
	if err != nil {
		return err
	}
	conn := pc.(*net.UDPConn)

	// Store for cleanup
	if label == "game" {
		gp.udpConn = conn
	} else {
		gp.voiceConn = conn
	}

	targetAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(targetPort)}
	rateLimiter := newRateTracker(DefaultMaxUDPPktPerSec)

	// Session map: track return path for each client
	type udpSession struct {
		clientAddr *net.UDPAddr
		serverConn *net.UDPConn // dedicated conn to game server for this client
		lastActive time.Time
	}
	sessions := &sync.Map{}

	// Cleanup stale sessions periodically
	gp.wg.Add(1)
	go func() {
		defer gp.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				sessions.Range(func(key, value any) bool {
					s := value.(*udpSession)
					if now.Sub(s.lastActive) > udpSessionTimeout {
						sessions.Delete(key)
						s.serverConn.Close()
					}
					return true
				})
			}
		}
	}()

	// Main read loop: client -> proxy -> game server
	gp.wg.Add(1)
	go func() {
		defer gp.wg.Done()
		defer conn.Close()

		buf := make([]byte, udpBufSize)
		for {
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				if gp.stopped.Load() || ctx.Err() != nil {
					return
				}
				continue
			}

			srcIP := clientAddr.IP.String()

			// Rate limit
			if !rateLimiter.allow(srcIP) {
				continue // silently drop
			}

			sessionKey := clientAddr.String()

			// Get or create session
			val, loaded := sessions.Load(sessionKey)
			var sess *udpSession
			if loaded {
				sess = val.(*udpSession)
				sess.lastActive = time.Now()
			} else {
				// Create a new UDP conn to the game server for return traffic
				srvConn, err := net.DialUDP("udp4", nil, targetAddr)
				if err != nil {
					gp.logger.Debug().Err(err).Str("label", label).Msg("failed to dial game server for UDP session")
					continue
				}
				sess = &udpSession{
					clientAddr: clientAddr,
					serverConn: srvConn,
					lastActive: time.Now(),
				}
				sessions.Store(sessionKey, sess)

				// Start return path goroutine: game server -> proxy -> client
				gp.wg.Add(1)
				go func(s *udpSession, key string) {
					defer gp.wg.Done()
					retBuf := make([]byte, udpBufSize)
					for {
						s.serverConn.SetReadDeadline(time.Now().Add(udpSessionTimeout))
						rn, err := s.serverConn.Read(retBuf)
						if err != nil {
							sessions.Delete(key)
							s.serverConn.Close()
							return
						}
						conn.WriteToUDP(retBuf[:rn], s.clientAddr)
					}
				}(sess, sessionKey)
			}

			// Forward to game server
			sess.serverConn.Write(buf[:n])
		}
	}()

	return nil
}

// ---- Rate Tracker ----

// rateTracker tracks per-IP request counts within a rolling second window.
type rateTracker struct {
	mu       sync.Mutex
	counts   map[string]*rateBucket
	maxPerSec int
}

type rateBucket struct {
	count    int
	windowStart time.Time
}

func newRateTracker(maxPerSec int) *rateTracker {
	return &rateTracker{
		counts:   make(map[string]*rateBucket),
		maxPerSec: maxPerSec,
	}
}

func (rt *rateTracker) allow(ip string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	b, exists := rt.counts[ip]
	if !exists || now.Sub(b.windowStart) >= time.Second {
		// New window
		rt.counts[ip] = &rateBucket{count: 1, windowStart: now}
		return true
	}

	b.count++
	return b.count <= rt.maxPerSec
}

// ---- Helpers ----

func extractIP(addr net.Addr) string {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		return udpAddr.IP.String()
	}
	host, _, _ := net.SplitHostPort(addr.String())
	return host
}
