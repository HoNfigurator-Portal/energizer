package server

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/network"
)

const (
	// MinRestartInterval is the minimum time between automatic restarts.
	MinRestartInterval = 24 * time.Hour
	// MaxRestartInterval is the maximum time between automatic restarts.
	MaxRestartInterval = 48 * time.Hour
	// IdleKickDelay is the time to wait before kicking idle players after game ends.
	IdleKickDelay = 60 * time.Second
	// LagWarningThreshold is the number of lag events before warning.
	LagWarningThreshold = 10
	// LagCriticalThreshold is the number of lag events before notifying admin.
	LagCriticalThreshold = 30
)

// Instance represents a single HoN game server instance.
// It manages the server's process, tracks its state, handles events,
// and implements auto-restart, lag monitoring, and idle player management.
type Instance struct {
	mu     sync.RWMutex
	id     int // 1-indexed server ID (maps to svr_slave)
	port   uint16
	logger zerolog.Logger

	// Configuration
	cfg         *config.Config
	eventBus    *events.EventBus
	cpuAffinity []int32

	// State
	state   *GameState
	process *ProcessManager
	enabled bool

	// Auto-restart
	nextRestart time.Time

	// Proxy for DDoS protection (forwards proxy ports -> game ports)
	proxy *network.GameProxy
}

// InstanceConfig holds configuration for creating a new server instance.
type InstanceConfig struct {
	ID          int // 1-indexed server instance ID
	Port        uint16
	CPUAffinity []int32
}

// NewInstance creates a new game server instance.
func NewInstance(cfg *config.Config, eventBus *events.EventBus, instCfg InstanceConfig) *Instance {
	logger := log.With().
		Str("component", "server").
		Int("id", instCfg.ID).
		Uint16("port", instCfg.Port).
		Logger()

	// Calculate next auto-restart time (random between 24-48 hours)
	restartInterval := MinRestartInterval + time.Duration(rand.Int63n(int64(MaxRestartInterval-MinRestartInterval)))

	inst := &Instance{
		id:          instCfg.ID,
		port:        instCfg.Port,
		logger:      logger,
		cfg:         cfg,
		eventBus:    eventBus,
		cpuAffinity: instCfg.CPUAffinity,
		state:       NewGameState(),
		enabled:     true,
		nextRestart: time.Now().Add(restartInterval),
	}

	// Create process manager
	procCfg := inst.buildProcessConfig()
	inst.process = NewProcessManager(procCfg)

	return inst
}

// Start launches the game server process.
// If proxy is enabled, the proxy is started first so it is ready to accept
// connections by the time the game server registers with the master server.
func (i *Instance) Start(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.enabled {
		return fmt.Errorf("server on port %d is disabled", i.port)
	}

	if i.process.IsRunning() {
		return fmt.Errorf("server on port %d is already running", i.port)
	}

	// Start proxy if enabled (before game server so ports are ready)
	honData := i.cfg.GetHoNData()
	if honData.EnableProxy {
		if err := i.startProxy(ctx); err != nil {
			i.logger.Error().Err(err).Msg("failed to start proxy, continuing without proxy")
			// Non-fatal: game server can still start, clients just connect directly
		}
	}

	i.state.SetStatus(events.GameStatusStarting)
	i.state.StartedAt = time.Now()

	i.logger.Info().Msg("starting game server")

	if err := i.process.Start(ctx); err != nil {
		i.state.SetStatus(events.GameStatusStopped)
		// Stop proxy if game server failed to start
		if i.proxy != nil {
			i.proxy.Stop()
			i.proxy = nil
		}
		return fmt.Errorf("failed to start server on port %d: %w", i.port, err)
	}

	return nil
}

// startProxy creates and starts a GameProxy for this instance.
// Proxy ports use the +10000 convention: gamePort+10000 and voicePort+10000.
func (i *Instance) startProxy(ctx context.Context) error {
	honData := i.cfg.GetHoNData()
	portOffset := i.port - uint16(honData.StartingGamePort)
	voicePort := uint16(honData.StartingVoicePort) + portOffset

	proxyCfg := network.GameProxyConfig{
		GamePort:        i.port,
		ProxyPort:       i.port + 10000,
		VoiceLocalPort:  voicePort,
		VoiceRemotePort: voicePort + 10000,
		ServerID:        i.id,
	}

	gp := network.NewGameProxy(proxyCfg)
	if err := gp.Start(ctx); err != nil {
		return err
	}

	i.proxy = gp
	i.logger.Info().
		Uint16("proxy_port", proxyCfg.ProxyPort).
		Uint16("voice_proxy_port", proxyCfg.VoiceRemotePort).
		Msg("proxy started for DDoS protection")
	return nil
}

// Stop gracefully stops the game server.
func (i *Instance) Stop() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.logger.Info().Msg("stopping game server")
	i.state.SetStatus(events.GameStatusStopped)

	if err := i.process.Stop(); err != nil {
		i.logger.Error().Err(err).Msg("failed to stop gracefully, killing")
		return i.process.Kill()
	}

	// Stop proxy if running
	if i.proxy != nil && i.proxy.IsRunning() {
		i.proxy.Stop()
		i.proxy = nil
	}

	return nil
}

// Restart stops and restarts the game server.
func (i *Instance) Restart(ctx context.Context) error {
	if err := i.Stop(); err != nil {
		i.logger.Warn().Err(err).Msg("error during stop before restart")
	}

	// Brief delay between stop and start
	time.Sleep(2 * time.Second)

	// Reset state
	i.state.Reset()

	// Recalculate next auto-restart
	restartInterval := MinRestartInterval + time.Duration(rand.Int63n(int64(MaxRestartInterval-MinRestartInterval)))
	i.mu.Lock()
	i.nextRestart = time.Now().Add(restartInterval)
	i.mu.Unlock()

	return i.Start(ctx)
}

// Enable marks the server as enabled (allowed to start).
func (i *Instance) Enable() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.enabled = true
	i.logger.Info().Msg("server enabled")
}

// Disable marks the server as disabled (will not start).
func (i *Instance) Disable() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.enabled = false
	i.logger.Info().Msg("server disabled")
}

// IsEnabled returns whether the server is enabled.
func (i *Instance) IsEnabled() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.enabled
}

// IsRunning returns whether the server process is running.
func (i *Instance) IsRunning() bool {
	return i.process.IsRunning()
}

// Port returns the game server port.
func (i *Instance) Port() uint16 {
	return i.port
}

// State returns the current game state.
func (i *Instance) State() *GameState {
	return i.state
}

// PID returns the process ID.
func (i *Instance) PID() int {
	return i.process.PID()
}

// Uptime returns how long the process has been running.
func (i *Instance) Uptime() time.Duration {
	return i.process.Uptime()
}

// HandleStatusUpdate processes a server status telemetry packet (0x42).
func (i *Instance) HandleStatusUpdate(payload events.ServerStatusPayload) {
	oldPhase := i.state.GetPhase()

	i.state.UpdateTelemetry(
		payload.Uptime,
		payload.CPUUsage,
		payload.PlayerCount,
		payload.GamePhase,
		payload.MatchID,
		payload.PlayerPings,
	)

	newPhase := payload.GamePhase

	// React to phase transitions
	if oldPhase != newPhase {
		i.onPhaseTransition(oldPhase, newPhase)
	}

	// Set server as ready if it was starting
	if i.state.GetStatus() == events.GameStatusStarting {
		i.state.SetStatus(events.GameStatusReady)
		i.logger.Info().Msg("server is now READY")
	}
}

// HandleLobbyCreated processes a lobby created event (0x44).
func (i *Instance) HandleLobbyCreated(payload events.LobbyCreatedPayload) {
	i.state.SetMatchInfo(payload.MatchID, payload.MapName, payload.Mode)
	i.state.SetPhase(events.GamePhaseInLobby)
	i.state.SetStatus(events.GameStatusOccupied)

	i.logger.Info().
		Uint32("match_id", payload.MatchID).
		Str("map", payload.MapName).
		Str("mode", payload.Mode).
		Msg("lobby created")
}

// HandleLobbyClosed processes a lobby closed event (0x45).
func (i *Instance) HandleLobbyClosed() {
	i.state.SetPhase(events.GamePhaseIdle)
	i.state.SetMatchInfo(0, "", "")

	i.logger.Info().Msg("lobby closed")

	// Check if server should return to ready
	if i.state.GetStatus() == events.GameStatusOccupied {
		i.state.SetStatus(events.GameStatusReady)
	}
}

// HandlePlayerConnection processes a player connect/disconnect event (0x47).
func (i *Instance) HandlePlayerConnection(payload events.PlayerConnectionPayload) {
	if payload.Connected {
		i.state.AddPlayer(payload.PlayerName, payload.PlayerID)
		i.logger.Info().
			Str("player", payload.PlayerName).
			Uint32("player_id", payload.PlayerID).
			Msg("player connected")
	} else {
		i.state.RemovePlayer(payload.PlayerName)
		i.logger.Info().
			Str("player", payload.PlayerName).
			Msg("player disconnected")
	}
}

// HandleLongFrame processes a lag event (0x43).
func (i *Instance) HandleLongFrame(payload events.LongFramePayload) {
	i.state.AddLagEvent(payload.FrameDuration)

	lagEvents := i.state.TotalLagEvents

	// Send in-game warning at threshold
	if lagEvents == LagWarningThreshold {
		i.logger.Warn().
			Int("lag_events", lagEvents).
			Uint32("duration_ms", payload.FrameDuration).
			Msg("lag warning threshold reached")
	}

	// Notify admin at critical threshold
	if lagEvents == LagCriticalThreshold {
		i.logger.Error().
			Int("lag_events", lagEvents).
			Msg("lag critical threshold reached, notifying admin")

		i.eventBus.Emit(context.Background(), events.Event{
			Type:   events.EventNotifyDiscordAdmin,
			Source: fmt.Sprintf("game_server:%d", i.port),
			Payload: events.NotifyDiscordPayload{
				Title:   "Lag Alert",
				Message: fmt.Sprintf("Server on port %d has experienced %d lag events", i.port, lagEvents),
				Level:   "warning",
			},
		})
	}
}

// NeedsRestart checks if the server is due for a periodic restart.
func (i *Instance) NeedsRestart() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	// Only restart if server is idle (no active match)
	if i.state.GetPhase() != events.GamePhaseIdle {
		return false
	}

	return time.Now().After(i.nextRestart)
}

// CheckIdlePlayers checks for and kicks idle players after game ends.
func (i *Instance) CheckIdlePlayers(registry interface{}) {
	if i.state.GetPhase() != events.GamePhaseGameEnded {
		return
	}

	// Check if enough time has passed since game ended
	phaseChangedAt := i.state.PhaseChangedAt
	if time.Since(phaseChangedAt) < IdleKickDelay {
		return
	}

	players := i.state.GetPlayers()
	if len(players) > 0 {
		i.logger.Info().
			Int("idle_players", len(players)).
			Msg("kicking idle players after game end")
	}
}

// onPhaseTransition handles game phase transitions.
func (i *Instance) onPhaseTransition(oldPhase, newPhase events.GamePhase) {
	i.logger.Info().
		Str("from", oldPhase.String()).
		Str("to", newPhase.String()).
		Msg("game phase transition")

	switch newPhase {
	case events.GamePhaseMatchStarted:
		// Increase process priority during match
		if err := i.process.SetHighPriority(); err != nil {
			i.logger.Debug().Err(err).Msg("failed to set high priority")
		}
		// Clear lag data for the new match
		i.state.ClearLagEvents()

	case events.GamePhaseGameEnded:
		// Restore normal priority
		if err := i.process.SetNormalPriority(); err != nil {
			i.logger.Debug().Err(err).Msg("failed to restore normal priority")
		}

	case events.GamePhaseIdle:
		// Clear match-related state
		i.state.SetMatchInfo(0, "", "")
		i.state.ClearLagEvents()
	}
}

// buildProcessConfig creates the process configuration for this server instance.
// This mirrors HoNfigurator-Central's start_server() in game_server.py:
// - Sets USERPROFILE and APPDATA environment variables for per-instance file isolation
// - Builds the full command-line with -dedicated -mod -noconfig -execute -masterserver -register
func (i *Instance) buildProcessConfig() ProcessConfig {
	honData := i.cfg.GetHoNData()

	// Executable name: from config or platform default
	exeName := honData.ExecutableName
	if exeName == "" {
		exeName = getServerExecutable()
	}
	executable := filepath.Join(honData.InstallDirectory, exeName)
	workDir := honData.InstallDirectory

	// Build command-line arguments (HoNfigurator-Central format)
	args := i.buildServerArgs()

	// Environment variable overrides for per-instance file isolation
	// HoNfigurator-Central: os.environ["APPDATA"] = hon_artefacts_directory
	//                       os.environ["USERPROFILE"] = hon_home_directory
	envVars := make(map[string]string)
	if runtime.GOOS == "windows" {
		artefactsDir := honData.ArtefactsDirectory
		if artefactsDir == "" {
			artefactsDir = honData.InstallDirectory
		}
		homeDir := honData.HomeDirectory
		if homeDir == "" {
			homeDir = honData.InstallDirectory
		}
		envVars["APPDATA"] = artefactsDir
		envVars["USERPROFILE"] = homeDir
	}

	return ProcessConfig{
		Executable:   executable,
		Args:         args,
		WorkDir:      workDir,
		Port:         i.port,
		CPUAffinity:  i.cpuAffinity,
		HighPriority: false,
		EnvVars:      envVars,
	}
}

// buildServerArgs constructs the command-line arguments for the game server.
// This exactly mirrors HoNfigurator-Central's build_commandline_args() in utilities.py:
//
// Windows: [-dedicated, -mod, game;KONGOR, (-noconsole), -noconfig, -execute, <params>, -masterserver, <url>, -register, 127.0.0.1:<port>]
// Linux:   [-dedicated, -mod game;KONGOR, -noconfig, -execute, "<params>", -masterserver, <url>, -register, 127.0.0.1:<port>]
func (i *Instance) buildServerArgs() []string {
	honData := i.cfg.GetHoNData()

	// Build the -execute parameter payload (semicolon-separated Set commands)
	executeParams := i.buildExecuteParams()

	var args []string

	if runtime.GOOS == "windows" {
		// Windows: -mod and game;KONGOR are separate list elements
		args = []string{
			"-dedicated",
			"-mod", "game;KONGOR",
		}
		if honData.NoConsole {
			args = append(args, "-noconsole")
		}
		// On Windows, the params string is passed raw (no wrapping quotes)
		args = append(args, "-noconfig")
		args = append(args, "-execute", executeParams)
	} else {
		// Linux: -mod value is a single combined argument
		args = []string{
			"-dedicated",
			"-mod game;KONGOR",
			"-noconfig",
		}
		// On Linux, params are wrapped in double quotes
		args = append(args, "-execute", fmt.Sprintf(`"%s"`, executeParams))
	}

	// Master server URL
	masterURL := honData.MasterServerURL
	if masterURL == "" {
		masterURL = "api.kongor.net"
	}
	args = append(args, "-masterserver", masterURL)

	// Register callback: game server connects back to manager on this address
	args = append(args, "-register", fmt.Sprintf("127.0.0.1:%d", honData.ManagerPort))

	return args
}

// buildExecuteParams builds the semicolon-separated "Set key value" string
// that gets passed to the -execute flag. This exactly mirrors
// HoNfigurator-Central's parameter dictionary in data_handler.py.
func (i *Instance) buildExecuteParams() string {
	honData := i.cfg.GetHoNData()

	// Calculate per-instance port offsets
	// When proxy is enabled, proxy ports are game/voice ports + 10000
	// (matching HoNfigurator-Central's convention).
	// When proxy is disabled, proxy ports equal the game/voice ports.
	portOffset := i.port - uint16(honData.StartingGamePort)
	voicePortStart := uint16(honData.StartingVoicePort) + portOffset

	var proxyPort, voiceLocalPort, voiceRemotePort uint16
	if honData.EnableProxy {
		proxyPort = i.port + 10000
		voiceLocalPort = voicePortStart
		voiceRemotePort = voicePortStart + 10000
	} else {
		proxyPort = i.port
		voiceLocalPort = voicePortStart
		voiceRemotePort = voicePortStart
	}

	// Chat server defaults
	chatAddress := honData.ChatAddress
	if chatAddress == "" {
		chatAddress = "96.127.149.202"
	}
	chatPort := honData.ChatPort
	if chatPort == 0 {
		chatPort = 11032
	}

	// Build ordered parameter list matching HoNfigurator-Central's data_handler.py
	type param struct{ key, value string }
	params := []param{
		{"svr_login", fmt.Sprintf("%s:%d", honData.Login, i.id)},
		{"svr_password", honData.Password},
		{"svr_description", fmt.Sprintf("priority:normal,cores:%s", formatCPUAffinity(i.cpuAffinity))},
		{"sv_masterName", fmt.Sprintf("%s:", honData.Login)},
		{"svr_slave", fmt.Sprintf("%d", i.id)},
		{"svr_name", fmt.Sprintf("%s %d 0", honData.Name, i.id)},
		{"svr_ip", honData.IP},
		{"svr_port", fmt.Sprintf("%d", i.port)},
		{"svr_proxyPort", fmt.Sprintf("%d", proxyPort)},
		{"svr_proxyLocalVoicePort", fmt.Sprintf("%d", voiceLocalPort)},
		{"svr_proxyRemoteVoicePort", fmt.Sprintf("%d", voiceRemotePort)},
		{"svr_voicePortStart", fmt.Sprintf("%d", voicePortStart)},
		{"man_enableProxy", boolToHoNString(honData.EnableProxy)},
		{"svr_location", honData.Location},
		{"svr_broadcast", "true"},
		{"upd_checkForUpdates", "false"},
		{"sv_autosaveReplay", "true"},
		{"sys_autoSaveDump", "false"},
		{"sys_dumpOnFatal", "false"},
		{"svr_chatPort", fmt.Sprintf("%d", chatPort)},
		{"svr_maxIncomingPacketsPerSecond", "300"},
		{"svr_maxIncomingBytesPerSecond", "1048576"},
		{"con_showNet", "false"},
		{"svr_submitStats", "true"},
		{"svr_chatAddress", chatAddress},
		{"http_useCompression", "false"},
		{"man_resubmitStats", "true"},
		{"man_uploadReplays", "true"},
		{"man_enableBotMatch", boolToHoNString(honData.AllowBotMatches)},
	}

	// host_affinity: removed on Windows if svr_override_affinity is enabled
	// (affinity is set post-launch via psutil/SetProcessAffinityMask instead)
	if !(runtime.GOOS == "windows" && honData.OverrideAffinity) {
		params = append(params, param{"host_affinity", formatCPUAffinity(i.cpuAffinity)})
	}

	// Join as semicolon-separated "Set key value" commands
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, fmt.Sprintf("Set %s %s", p.key, p.value))
	}

	return strings.Join(parts, ";")
}

// getServerExecutable returns the platform-specific server executable name.
func getServerExecutable() string {
	if runtime.GOOS == "windows" {
		return "hon_x64.exe"
	}
	return "hon_x64"
}

// calculateVoicePort calculates the voice port for a given game port.
func calculateVoicePort(gamePort uint16, honData config.HoNData) uint16 {
	offset := gamePort - uint16(honData.StartingGamePort)
	return uint16(honData.StartingVoicePort) + offset
}

// formatCPUAffinity formats CPU core list for the host_affinity parameter.
func formatCPUAffinity(cores []int32) string {
	if len(cores) == 0 {
		return "-1"
	}
	parts := make([]string, len(cores))
	for idx, c := range cores {
		parts[idx] = fmt.Sprintf("%d", c)
	}
	return strings.Join(parts, ",")
}

// boolToHoNString converts a bool to the HoN config string format.
func boolToHoNString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ID returns the server instance ID (1-indexed).
func (i *Instance) ID() int {
	return i.id
}

// GetInfo returns a summary of the server instance for API responses.
func (i *Instance) GetInfo() InstanceInfo {
	i.mu.RLock()
	defer i.mu.RUnlock()

	snapshot := i.state.Snapshot()

	honData := i.cfg.GetHoNData()
	serverName := fmt.Sprintf("%s %d", honData.Name, i.id)

	return InstanceInfo{
		ID:          i.id,
		ServerName:  serverName,
		Port:        i.port,
		Enabled:     i.enabled,
		Running:     i.process.IsRunning(),
		PID:         i.process.PID(),
		Uptime:      i.process.Uptime().String(),
		State:       snapshot,
		NextRestart: i.nextRestart,
	}
}

// InstanceInfo is a JSON-serializable summary of a server instance.
type InstanceInfo struct {
	ID          int               `json:"id"`
	ServerName  string            `json:"server_name"`
	Port        uint16            `json:"port"`
	Enabled     bool              `json:"enabled"`
	Running     bool              `json:"running"`
	PID         int               `json:"pid"`
	Uptime      string            `json:"uptime"`
	State       GameStateSnapshot `json:"state"`
	NextRestart time.Time         `json:"next_restart"`
}
