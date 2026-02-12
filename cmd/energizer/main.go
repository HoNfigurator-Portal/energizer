// Energizer - HoN Game Server Manager & API
// A high-performance rewrite of HoNfigurator-Central in Go.
//
// Energizer manages the lifecycle of HoN game server instances,
// negotiates TCP connections with upstream authentication services,
// exposes a REST API for remote management, and publishes real-time
// telemetry via MQTT.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/api"
	"github.com/energizer-project/energizer/internal/cli"
	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/connector"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/health"
	"github.com/energizer-project/energizer/internal/network"
	"github.com/energizer-project/energizer/internal/scheduler"
	"github.com/energizer-project/energizer/internal/server"
	"github.com/energizer-project/energizer/internal/telemetry"
	"github.com/energizer-project/energizer/internal/util"
)

const (
	AppName    = "Energizer"
	AppVersion = "1.0.0"
	Banner     = `
  ______                       _              
 |  ____|                     (_)             
 | |__   _ __   ___ _ __ __ _ _ _______ _ __ 
 |  __| | '_ \ / _ \ '__/ _' | |_  / _ \ '__|
 | |____| | | |  __/ | | (_| | |/ /  __/ |   
 |______|_| |_|\___|_|  \__, |_/___\___|_|   
                          __/ |               
                         |___/  v%s
 HoN Game Server Manager & API
`
)

func main() {
	// Print banner
	fmt.Printf(Banner, AppVersion)
	fmt.Println()

	// Initialize logger with defaults first (will be reconfigured after config load)
	if err := util.InitLogger(util.DefaultLogConfig()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	log.Info().
		Str("version", AppVersion).
		Str("platform", runtime.GOOS).
		Str("arch", runtime.GOARCH).
		Int("cpus", runtime.NumCPU()).
		Msg("starting Energizer")

	// Load configuration
	cfg, err := config.Load(config.DefaultConfigDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Re-initialize logger with config-based settings
	logCfg := util.LogConfig{
		Level:      cfg.ApplicationData.Logging.Level,
		Directory:  cfg.ApplicationData.Logging.Directory,
		MaxSizeMB:  cfg.ApplicationData.Logging.MaxSizeMB,
		MaxBackups: cfg.ApplicationData.Logging.MaxBackups,
		Console:    true,
	}
	if err := util.InitLogger(logCfg); err != nil {
		log.Warn().Err(err).Msg("failed to reconfigure logger, using defaults")
	}

	// Validate configuration
	validation := config.Validate(cfg)
	for _, w := range validation.Warnings {
		log.Warn().Str("field", w.Field).Msg(w.Message)
	}
	if !validation.IsValid() {
		for _, e := range validation.Errors {
			log.Error().Str("field", e.Field).Msg(e.Message)
		}

		if cfg.IsFirstRun() {
			log.Info().Msg("first run detected, launching setup wizard")
			if err := config.RunSetupWizard(cfg); err != nil {
				log.Fatal().Err(err).Msg("setup wizard failed")
			}
		} else {
			log.Fatal().Msg("configuration validation failed, please fix the errors above")
		}
	}

	// Log system info
	sysInfo := util.GetSystemInfo()
	log.Info().
		Str("hostname", sysInfo.Hostname).
		Str("os", sysInfo.OS).
		Str("cpu", sysInfo.CPUModel).
		Int("cores", sysInfo.CPUCores).
		Uint64("memory_mb", sysInfo.TotalMemory).
		Msg("system information")

	// Auto-detect server IP if not configured.
	// The game server uses svr_ip to register itself with the master server.
	// Without a valid IP, the server will not appear in the game client list.
	honData := cfg.GetHoNData()
	if honData.IP == "" {
		masterAddr := honData.MasterServerURL
		// If the master server resolves to a private/local IP, use a public
		// DNS server as the dial target instead — connecting to yourself
		// won't reveal your external IP.
		if isLocalAddress(masterAddr) {
			masterAddr = "8.8.8.8" // fallback: use Google DNS as a routable target
		}
		detectedIP := detectServerIP(masterAddr)
		if detectedIP != "" {
			log.Info().Str("svr_ip", detectedIP).Msg("auto-detected server IP (svr_ip was empty)")
			honData.IP = detectedIP
			cfg.SetHoNData(honData)
			if err := cfg.Save(); err != nil {
				log.Warn().Err(err).Msg("failed to save auto-detected IP to config")
			}
		} else {
			log.Warn().Msg("svr_ip is empty and auto-detection failed — game servers may not be visible to clients")
		}
	} else {
		log.Info().Str("svr_ip", honData.IP).Msg("using configured server IP")
	}

	// Cleanup leftover game server processes from a previous run
	// This prevents port conflicts and ensures clean state
	cleanupLeftoverProcesses(cfg)

	// Also try PID-based cleanup (more reliable than process name matching)
	// This will be configured after manager creation

	// Create root context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize core components
	eventBus := events.NewEventBus()

	// Initialize server manager (central orchestrator)
	mgr, err := server.NewManager(cfg, eventBus)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server manager")
	}

	// PID-based cleanup of leftover game servers
	mgr.CleanupLeftoverServers()

	// Initialize connectors
	masterConn := connector.NewMasterServerConnector(cfg, eventBus)
	chatConn := connector.NewChatServerConnector(cfg, eventBus)

	// Initialize network listeners
	tcpListener := network.NewTCPListener(cfg, eventBus, mgr)
	udpListener := network.NewUDPAutoPingListener(cfg)

	// Initialize REST API
	apiServer := api.NewServer(cfg, eventBus, mgr)

	// Initialize health check manager
	healthMgr := health.NewManager(cfg, eventBus, mgr, masterConn)

	// Initialize MQTT telemetry
	var mqttHandler *telemetry.MQTTHandler
	if cfg.ApplicationData.MQTT.Enabled {
		mqttHandler, err = telemetry.NewMQTTHandler(cfg, eventBus)
		if err != nil {
			log.Warn().Err(err).Msg("failed to initialize MQTT, telemetry disabled")
		}
	}

	// Initialize scheduler
	sched := scheduler.NewScheduler(cfg, eventBus)

	// Initialize CLI
	cliHandler := cli.NewCLI(cfg, eventBus, mgr)

	// ---------------------------------------------------------------
	// Launch all concurrent tasks (mirrors the 5 Python asyncio tasks
	// plus additional goroutines for health, MQTT, scheduler, CLI)
	// ---------------------------------------------------------------
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	// Task 1: Manage upstream connections (master server auth + chat server)
	// These are NON-FATAL: game servers handle their own upstream auth via -masterserver flag.
	// The manager's auth is for replay uploads, stats submission, and chat server.
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Msg("starting upstream connection manager")
		if err := masterConn.ManageConnection(ctx); err != nil {
			log.Warn().Err(err).Msg("master server connection failed (non-fatal, game servers manage their own upstream auth)")
			// Don't send to errCh - game servers connect directly to master server
			// via the -masterserver flag. This connector is only for manager-level
			// features (replay uploads, stats, patch checks).
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait for master server auth before connecting to chat
		time.Sleep(2 * time.Second)
		log.Info().Msg("starting chat server connector")
		if err := chatConn.ManageConnection(ctx, masterConn); err != nil {
			log.Warn().Err(err).Msg("chat server connection failed (non-fatal)")
			// Don't send to errCh - chat server is for admin notifications only
		}
	}()

	// Task 2: Start game servers (non-fatal: failures are logged but don't crash the app)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Msg("starting game servers")
		if err := mgr.StartAll(ctx); err != nil {
			log.Warn().Err(err).Msg("some game servers failed to start (non-fatal)")
			// Don't send to errCh - game server failures should not crash the manager.
			// The health check system will attempt restarts automatically.
		}
	}()

	// Task 3: Start REST API server (with retry for port binding)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Int("port", cfg.HoNData.APIPort).Msg("starting REST API server")
		if err := startWithRetry(ctx, "API server", apiServer.Start, 15); err != nil {
			log.Warn().Err(err).Msg("API server failed after retries (non-fatal)")
		}
	}()

	// Task 4: Start TCP listener for game server connections (with retry for port binding)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Int("port", cfg.HoNData.ManagerPort).Msg("starting TCP listener")
		if err := startWithRetry(ctx, "TCP listener", tcpListener.Start, 15); err != nil {
			log.Error().Err(err).Msg("TCP listener failed after retries")
			errCh <- fmt.Errorf("tcp listener: %w", err)
		}
	}()

	// Task 5: Start UDP AutoPing listener (with retry for port binding)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Msg("starting UDP AutoPing listener")
		if err := startWithRetry(ctx, "UDP AutoPing", udpListener.Start, 15); err != nil {
			log.Warn().Err(err).Msg("UDP AutoPing listener failed after retries (non-fatal)")
		}
	}()

	// Task 6: Health check manager
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Msg("starting health check manager")
		healthMgr.Start(ctx)
	}()

	// Task 7: MQTT telemetry
	if mqttHandler != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info().Msg("starting MQTT telemetry")
			if err := mqttHandler.Start(ctx); err != nil {
				log.Warn().Err(err).Msg("MQTT telemetry failed")
			}
		}()
	}

	// Task 8: Scheduler (replay cleanup, stats)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Msg("starting task scheduler")
		sched.Start(ctx)
	}()

	// Task 9: Interactive CLI
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info().Msg("starting interactive CLI")
		cliHandler.Start(ctx)
	}()

	// ---------------------------------------------------------------
	// Graceful shutdown handling
	// ---------------------------------------------------------------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case err := <-errCh:
		log.Error().Err(err).Msg("critical error, initiating shutdown")
	}

	log.Info().Msg("initiating graceful shutdown...")

	// Cancel the root context to signal all goroutines
	cancel()

	// Emit shutdown event
	eventBus.Emit(ctx, events.Event{
		Type:   events.EventShutdown,
		Source: "main",
	})

	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("all tasks stopped gracefully")
	case <-time.After(30 * time.Second):
		log.Warn().Msg("shutdown timed out after 30 seconds, forcing exit")
	}

	// Remove PID file on clean shutdown
	mgr.RemovePIDFile()

	// Stop the event bus last
	eventBus.Stop()

	// Shutdown MQTT
	if mqttHandler != nil {
		mqttHandler.PublishShutdown()
	}

	log.Info().Msg("Energizer stopped")
}

// cleanupLeftoverProcesses kills any hon_x64 and old energizer processes from a previous run.
// This prevents port conflicts when Energizer is restarted.
func cleanupLeftoverProcesses(cfg *config.Config) {
	exeName := cfg.GetHoNData().ExecutableName
	if exeName == "" {
		if runtime.GOOS == "windows" {
			exeName = "hon_x64.exe"
		} else {
			exeName = "hon_x64"
		}
	}

	cleaned := false
	myPID := os.Getpid()

	if runtime.GOOS == "windows" {
		// Kill leftover Energizer processes (not ourselves)
		// Use PowerShell to find and kill other energizer.exe by PID
		psCmd := fmt.Sprintf(
			"Get-Process -Name energizer -ErrorAction SilentlyContinue | Where-Object { $_.Id -ne %d } | Stop-Process -Force -ErrorAction SilentlyContinue",
			myPID,
		)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
		if err := cmd.Run(); err == nil {
			log.Info().Msg("cleaned up leftover Energizer processes")
			cleaned = true
		}

		// Kill leftover game servers
		cmd = exec.Command("taskkill", "/F", "/IM", exeName)
		if err := cmd.Run(); err == nil {
			log.Info().Str("executable", exeName).Msg("cleaned up leftover game server processes")
			cleaned = true
		}
	} else {
		cmd := exec.Command("pkill", "-9", "-f", exeName)
		if err := cmd.Run(); err == nil {
			log.Info().Str("executable", exeName).Msg("cleaned up leftover game server processes")
			cleaned = true
		}
	}

	if cleaned {
		// Give processes and OS time to release ports
		log.Info().Msg("waiting for ports to be released...")
		time.Sleep(3 * time.Second)
	}
}

// startWithRetry attempts to start a listener/server with retry on bind errors.
// Uses a fixed 3-second interval between retries. This gives enough time
// for Windows to release sockets after a process is force-killed.
// Returns nil on success, or the last error after all retries fail.
func startWithRetry(ctx context.Context, name string, startFn func(context.Context) error, maxRetries int) error {
	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = startFn(ctx)
		if lastErr == nil {
			return nil
		}
		if i < maxRetries {
			log.Warn().Err(lastErr).Str("component", name).Int("retry", i+1).Int("max", maxRetries).Msg("bind failed, retrying in 3s...")
			time.Sleep(3 * time.Second)
		}
	}
	return lastErr
}

// detectServerIP determines the local IP address that should be used for game
// server registration. It works by opening a UDP "connection" to the master
// server (or any routable address) — no actual packets are sent — and reading
// back which local IP the OS selected for the route. This is the most reliable
// cross-platform method to detect the correct network interface IP.
func detectServerIP(masterServerURL string) string {
	// Strip http(s):// scheme if present
	host := masterServerURL
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	// Strip path component if any
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}

	// Ensure host:port for net.Dial
	if !strings.Contains(host, ":") {
		host = host + ":80"
	}

	conn, err := net.DialTimeout("udp4", host, 5*time.Second)
	if err != nil {
		log.Debug().Err(err).Msg("UDP dial to master server failed, falling back to interface scan")
		return detectLocalIP()
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	ip := localAddr.IP.String()

	// Sanity check: skip loopback or unspecified
	if ip == "127.0.0.1" || ip == "0.0.0.0" || ip == "" {
		log.Debug().Str("ip", ip).Msg("UDP dial returned unusable IP, falling back to interface scan")
		return detectLocalIP()
	}

	return ip
}

// isLocalAddress returns true if the given host (host:port or just host)
// resolves to a private, loopback, or link-local IP address, or matches
// one of the machine's own interface addresses.
func isLocalAddress(addr string) bool {
	host := strings.TrimPrefix(addr, "https://")
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	// Strip port
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Try DNS resolve
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return false
		}
		ip = ips[0]
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Check RFC1918 private ranges
	privateRanges := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	// Check if the IP belongs to this machine
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// detectLocalIP scans network interfaces and returns the first non-loopback
// IPv4 address found. Used as a fallback when route-based detection fails.
func detectLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Debug().Err(err).Msg("failed to enumerate network interfaces")
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}
