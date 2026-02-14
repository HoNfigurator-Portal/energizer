package server

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/network"
)

const pidFileName = "energizer_servers.pid"

// Manager is the central orchestrator for all game server instances.
// It replaces the Python GameServerManager (~1335 lines) and manages
// the fleet of game servers, their connections, lifecycle events,
// and health monitoring.
type Manager struct {
	mu sync.RWMutex

	cfg      *config.Config
	eventBus *events.EventBus

	// Server instances indexed by port
	servers map[uint16]*Instance

	// Connection registry
	connRegistry *network.ConnectionRegistry

	// Startup semaphore to limit concurrent server starts
	startSemaphore chan struct{}

	// Server version info
	honVersion     string
	managerVersion string
	publicIP       string
}

// NewManager creates and initializes the server manager.
func NewManager(cfg *config.Config, eventBus *events.EventBus) (*Manager, error) {
	maxConcurrent := cfg.GetHoNData().MaxConcurrentStarts
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}

	mgr := &Manager{
		cfg:            cfg,
		eventBus:       eventBus,
		servers:        make(map[uint16]*Instance),
		connRegistry:   network.NewConnectionRegistry(),
		startSemaphore: make(chan struct{}, maxConcurrent),
		managerVersion: "1.0.0",
	}

	log.Info().Int("max_concurrent_starts", maxConcurrent).Msg("server startup concurrency configured")

	// Subscribe to events
	mgr.subscribeEvents()

	// Pre-create server instances
	mgr.initializeServers()

	return mgr, nil
}

// subscribeEvents registers all event handlers on the EventBus.
func (m *Manager) subscribeEvents() {
	bus := m.eventBus

	// Server lifecycle events
	bus.Subscribe(events.EventServerAnnounce, "manager.serverAnnounce", m.onServerAnnounce)
	bus.Subscribe(events.EventServerClosed, "manager.serverClosed", m.onServerClosed)
	bus.Subscribe(events.EventServerStatus, "manager.serverStatus", m.onServerStatus)
	bus.Subscribe(events.EventLobbyCreated, "manager.lobbyCreated", m.onLobbyCreated)
	bus.Subscribe(events.EventLobbyClosed, "manager.lobbyClosed", m.onLobbyClosed)
	bus.Subscribe(events.EventPlayerConnection, "manager.playerConnection", m.onPlayerConnection)
	bus.Subscribe(events.EventLongFrame, "manager.longFrame", m.onLongFrame)
	bus.Subscribe(events.EventReplayStatus, "manager.replayStatus", m.onReplayStatus)
	bus.Subscribe(events.EventCowMasterResponse, "manager.cowmasterResponse", m.onCowMasterResponse)

	// Command events
	bus.Subscribe(events.EventShutdownServer, "manager.shutdownServer", m.onCmdShutdownServer)
	bus.Subscribe(events.EventWakeServer, "manager.wakeServer", m.onCmdWakeServer)
	bus.Subscribe(events.EventSleepServer, "manager.sleepServer", m.onCmdSleepServer)
	bus.Subscribe(events.EventMessageServer, "manager.messageServer", m.onCmdMessageServer)

	// Config events
	bus.Subscribe(events.EventConfigChanged, "manager.configChanged", m.onConfigChanged)

	// Shutdown
	bus.Subscribe(events.EventShutdown, "manager.shutdown", m.onShutdown)

	log.Debug().Msg("manager event subscriptions registered")
}

// initializeServers pre-creates server instances based on configuration.
func (m *Manager) initializeServers() {
	honData := m.cfg.GetHoNData()
	totalServers := honData.TotalServers
	startPort := uint16(honData.StartingGamePort)

	log.Info().
		Int("total", totalServers).
		Uint16("start_port", startPort).
		Msg("initializing server instances")

	for i := 0; i < totalServers; i++ {
		serverID := i + 1 // 1-indexed, matching HoNfigurator-Central's svr_slave
		port := startPort + uint16(i)
		affinity := calculateCPUAffinity(i, honData.ServersPerCore)

		inst := NewInstance(m.cfg, m.eventBus, InstanceConfig{
			ID:          serverID,
			Port:        port,
			CPUAffinity: affinity,
		})

		m.servers[port] = inst
		log.Debug().Int("id", serverID).Uint16("port", port).Ints32("affinity", affinity).Msg("server instance created")
	}
}

// StartAll launches all configured game servers in batches.
// Each batch starts up to MaxConcurrentStarts servers, then waits for them
// to reach READY state (or timeout) before starting the next batch.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	servers := make([]*Instance, 0, len(m.servers))
	for _, inst := range m.servers {
		servers = append(servers, inst)
	}
	m.mu.RUnlock()

	// Sort by port for deterministic startup order
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Port() < servers[j].Port()
	})

	batchSize := cap(m.startSemaphore)
	totalCount := len(servers)

	log.Info().Int("count", totalCount).Int("batch_size", batchSize).Msg("starting all game servers")

	var totalSuccess, totalFail int

	for batchStart := 0; batchStart < totalCount; batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > totalCount {
			batchEnd = totalCount
		}
		batch := servers[batchStart:batchEnd]
		batchNum := (batchStart / batchSize) + 1

		log.Info().
			Int("batch", batchNum).
			Int("servers", len(batch)).
			Int("from", batchStart+1).
			Int("to", batchEnd).
			Msg("starting batch")

		// Start all servers in this batch concurrently
		var wg sync.WaitGroup
		var batchSuccess, batchFail int
		var mu sync.Mutex

		for _, inst := range batch {
			inst := inst
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := inst.Start(ctx); err != nil {
					log.Warn().Err(err).Uint16("port", inst.Port()).Msg("failed to start server")
					mu.Lock()
					batchFail++
					mu.Unlock()
					return
				}
				mu.Lock()
				batchSuccess++
				mu.Unlock()
			}()
		}
		wg.Wait()

		totalSuccess += batchSuccess
		totalFail += batchFail

		log.Info().
			Int("batch", batchNum).
			Int("success", batchSuccess).
			Int("failed", batchFail).
			Msg("batch processes spawned")

		// Wait for servers in this batch to become READY before starting next batch
		if batchEnd < totalCount && batchSuccess > 0 {
			m.waitForBatchReady(ctx, batch, 120*time.Second)
		}
	}

	log.Info().
		Int("success", totalSuccess).
		Int("failed", totalFail).
		Int("total", totalCount).
		Msg("game server startup complete")

	if totalFail > 0 && totalSuccess == 0 {
		return fmt.Errorf("all %d servers failed to start", totalFail)
	}

	// Save PIDs to file for cleanup on restart
	m.savePIDFile()

	return nil
}

// waitForBatchReady waits until all servers in the batch reach READY state
// or the timeout expires. This ensures a batch is fully loaded before the
// next batch starts, preventing CPU/memory overload from too many servers
// loading game assets simultaneously.
func (m *Manager) waitForBatchReady(ctx context.Context, batch []*Instance, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	log.Info().Int("count", len(batch)).Msg("waiting for batch to become ready")

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			log.Warn().Msg("batch ready timeout reached, proceeding with next batch")
			return
		case <-ticker.C:
			readyCount := 0
			for _, inst := range batch {
				status := inst.State().GetStatus()
				if status == events.GameStatusReady ||
					status == events.GameStatusOccupied ||
					status == events.GameStatusSleeping ||
					status == events.GameStatusStopped {
					readyCount++
				}
			}
			if readyCount >= len(batch) {
				log.Info().Int("ready", readyCount).Msg("all servers in batch are ready, proceeding")
				return
			}
			log.Debug().Int("ready", readyCount).Int("total", len(batch)).Msg("waiting for batch servers")
		}
	}
}

// CleanupLeftoverServers kills game servers from a previous run using the PID file.
// This should be called BEFORE starting new servers.
func (m *Manager) CleanupLeftoverServers() {
	pidFile := filepath.Join("config", pidFileName)
	f, err := os.Open(pidFile)
	if err != nil {
		return // No PID file = no leftover servers
	}
	defer f.Close()

	killed := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		// Try to kill the process
		terminateProcessPlatform(pid)
		killed++
	}

	if killed > 0 {
		log.Info().Int("count", killed).Msg("cleaned up leftover game server processes from PID file")
		// Wait for ports to be released
		time.Sleep(3 * time.Second)
	}

	// Remove the PID file
	os.Remove(pidFile)
}

// savePIDFile writes current game server PIDs to a file for cleanup on restart.
func (m *Manager) savePIDFile() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pidFile := filepath.Join("config", pidFileName)
	var lines []string
	lines = append(lines, "# Energizer game server PIDs - do not edit")
	for _, inst := range m.servers {
		if inst.process.IsRunning() {
			lines = append(lines, strconv.Itoa(inst.process.PID()))
		}
	}

	if len(lines) <= 1 {
		return // No running servers
	}

	os.WriteFile(pidFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// RemovePIDFile removes the PID file (called during clean shutdown).
func (m *Manager) RemovePIDFile() {
	pidFile := filepath.Join("config", pidFileName)
	os.Remove(pidFile)
}

// StopAll stops all running game servers.
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	log.Info().Msg("stopping all game servers")

	var wg sync.WaitGroup
	for _, inst := range m.servers {
		inst := inst
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := inst.Stop(); err != nil {
				log.Error().Err(err).Uint16("port", inst.Port()).Msg("failed to stop server")
			}
		}()
	}
	wg.Wait()

	// Close all connections
	m.connRegistry.CloseAll()

	log.Info().Msg("all game servers stopped")
}

// GetInstance returns a server instance by port.
func (m *Manager) GetInstance(port uint16) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.servers[port]
	return inst, ok
}

// GetAllInstances returns all server instances.
func (m *Manager) GetAllInstances() map[uint16]*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[uint16]*Instance, len(m.servers))
	for k, v := range m.servers {
		result[k] = v
	}
	return result
}

// GetConnectionRegistry returns the connection registry.
func (m *Manager) GetConnectionRegistry() *network.ConnectionRegistry {
	return m.connRegistry
}

// HandleServerEvent handles events dispatched directly from the TCP listener.
func (m *Manager) HandleServerEvent(ctx context.Context, event *events.Event) {
	// This is called directly (not through EventBus) for immediate processing.
	// The EventBus handlers will also fire asynchronously.
}

// GetAllInfo returns status information for all servers (for API), sorted by ID.
func (m *Manager) GetAllInfo() []InstanceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := make([]InstanceInfo, 0, len(m.servers))
	for _, inst := range m.servers {
		info = append(info, inst.GetInfo())
	}
	sort.Slice(info, func(i, j int) bool {
		return info[i].ID < info[j].ID
	})
	return info
}

// GetTotalServers returns the total number of configured servers.
func (m *Manager) GetTotalServers() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.servers)
}

// GetRunningCount returns the number of currently running servers.
func (m *Manager) GetRunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, inst := range m.servers {
		if inst.IsRunning() {
			count++
		}
	}
	return count
}

// GetOccupiedCount returns the number of servers with active matches.
func (m *Manager) GetOccupiedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, inst := range m.servers {
		if inst.State().GetStatus() == events.GameStatusOccupied {
			count++
		}
	}
	return count
}

// SetPublicIP updates the public IP address.
func (m *Manager) SetPublicIP(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publicIP = ip
}

// GetPublicIP returns the current public IP.
func (m *Manager) GetPublicIP() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.publicIP
}

// SetHoNVersion updates the HoN server version.
func (m *Manager) SetHoNVersion(version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.honVersion = version
}

// --- Event Handlers ---

func (m *Manager) onServerAnnounce(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerAnnouncePayload)
	if !ok {
		return fmt.Errorf("invalid server announce payload")
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.State().SetStatus(events.GameStatusReady)
		log.Info().Uint16("port", payload.Port).Msg("server announced and registered")
	}
	return nil
}

func (m *Manager) onServerClosed(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerAnnouncePayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.State().SetStatus(events.GameStatusStopped)
		log.Info().Uint16("port", payload.Port).Msg("server closed")
	}
	return nil
}

func (m *Manager) onServerStatus(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerStatusPayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.HandleStatusUpdate(payload)
	}
	return nil
}

func (m *Manager) onLobbyCreated(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.LobbyCreatedPayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.HandleLobbyCreated(payload)
	}
	return nil
}

func (m *Manager) onLobbyClosed(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerAnnouncePayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.HandleLobbyClosed()
	}
	return nil
}

func (m *Manager) onPlayerConnection(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.PlayerConnectionPayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.HandlePlayerConnection(payload)
	}
	return nil
}

func (m *Manager) onLongFrame(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.LongFramePayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.HandleLongFrame(payload)
	}
	return nil
}

func (m *Manager) onReplayStatus(ctx context.Context, event events.Event) error {
	// Handle replay status updates
	return nil
}

func (m *Manager) onCowMasterResponse(ctx context.Context, event events.Event) error {
	// Handle CowMaster fork responses
	return nil
}

func (m *Manager) onCmdShutdownServer(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerCommandPayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		return inst.Stop()
	}
	return fmt.Errorf("server not found on port %d", payload.Port)
}

func (m *Manager) onCmdWakeServer(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerCommandPayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		if inst.State().GetStatus() == events.GameStatusSleeping {
			inst.State().SetStatus(events.GameStatusReady)
			log.Info().Uint16("port", payload.Port).Msg("server woken up")
		}
	}
	return nil
}

func (m *Manager) onCmdSleepServer(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerCommandPayload)
	if !ok {
		return nil
	}

	if inst, ok := m.GetInstance(payload.Port); ok {
		inst.State().SetStatus(events.GameStatusSleeping)
		log.Info().Uint16("port", payload.Port).Msg("server put to sleep")
	}
	return nil
}

func (m *Manager) onCmdMessageServer(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerCommandPayload)
	if !ok {
		return nil
	}

	conn, ok := m.connRegistry.Get(payload.Port)
	if !ok {
		return fmt.Errorf("no connection for port %d", payload.Port)
	}

	if len(payload.Args) > 0 {
		return conn.SendMessage(payload.Args[0])
	}
	return nil
}

func (m *Manager) onConfigChanged(ctx context.Context, event events.Event) error {
	log.Info().Msg("configuration changed, reloading...")
	// Re-read config and update servers as needed
	return nil
}

func (m *Manager) onShutdown(ctx context.Context, event events.Event) error {
	log.Info().Msg("shutdown event received, stopping all servers")
	m.StopAll()
	return nil
}

// calculateCPUAffinity assigns CPU cores to a server based on its index.
func calculateCPUAffinity(serverIndex int, serversPerCore int) []int32 {
	if serversPerCore <= 0 {
		return nil
	}
	coreIndex := int32(serverIndex / serversPerCore)
	return []int32{coreIndex}
}

// AddServers dynamically adds new server instances.
func (m *Manager) AddServers(ctx context.Context, count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	honData := m.cfg.GetHoNData()

	// Find the highest current port
	maxPort := uint16(honData.StartingGamePort)
	for port := range m.servers {
		if port >= maxPort {
			maxPort = port + 1
		}
	}

	for i := 0; i < count; i++ {
		port := maxPort + uint16(i)
		serverIdx := len(m.servers) + i
		serverID := serverIdx + 1
		affinity := calculateCPUAffinity(serverIdx, honData.ServersPerCore)

		inst := NewInstance(m.cfg, m.eventBus, InstanceConfig{
			ID:          serverID,
			Port:        port,
			CPUAffinity: affinity,
		})

		m.servers[port] = inst

		go func(inst *Instance) {
			if err := inst.Start(ctx); err != nil {
				log.Error().Err(err).Uint16("port", inst.Port()).Msg("failed to start new server")
			}
		}(inst)
	}

	// Persist new total to config so it survives restart
	honData.TotalServers = len(m.servers)
	m.cfg.SetHoNData(honData)
	if err := m.cfg.Save(); err != nil {
		log.Warn().Err(err).Msg("failed to save config after adding servers")
	}

	log.Info().Int("count", count).Msg("added new servers")
	return nil
}

// RemoveServers removes server instances (stops and removes from pool).
func (m *Manager) RemoveServers(ports []uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, port := range ports {
		if inst, ok := m.servers[port]; ok {
			inst.Stop()
			delete(m.servers, port)
			log.Info().Uint16("port", port).Msg("server removed from pool")
		}
	}

	// Persist new total to config so it survives restart
	honData := m.cfg.GetHoNData()
	honData.TotalServers = len(m.servers)
	m.cfg.SetHoNData(honData)
	if err := m.cfg.Save(); err != nil {
		log.Warn().Err(err).Msg("failed to save config after removing servers")
	}

	return nil
}
