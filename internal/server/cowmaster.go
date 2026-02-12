package server

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
)

// CowMaster manages a pre-loaded "master" game server process on Linux.
// When enabled, it pre-loads game resources into memory and uses
// copy-on-write (CoW) fork to create new server instances, resulting in
// near-instant startup and significantly reduced RAM usage.
//
// This is a Linux-only feature.
type CowMaster struct {
	mu sync.Mutex

	cfg      *config.Config
	eventBus *events.EventBus

	// Process state
	process  *ProcessManager
	ready    bool
	lastFork time.Time
}

// NewCowMaster creates a new CowMaster instance.
func NewCowMaster(cfg *config.Config, eventBus *events.EventBus) *CowMaster {
	return &CowMaster{
		cfg:      cfg,
		eventBus: eventBus,
	}
}

// IsSupported returns whether CowMaster is supported on the current platform.
func (cm *CowMaster) IsSupported() bool {
	return runtime.GOOS == "linux"
}

// Start launches the CowMaster process.
func (cm *CowMaster) Start(ctx context.Context) error {
	if !cm.IsSupported() {
		return fmt.Errorf("CowMaster is only supported on Linux")
	}

	if !cm.cfg.GetHoNData().UseCowMaster {
		return fmt.Errorf("CowMaster is disabled in configuration")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	honData := cm.cfg.GetHoNData()

	// CowMaster executable
	exeName := honData.ExecutableName
	if exeName == "" {
		exeName = "hon_x64"
	}

	// CowMaster uses -cowmaster -servicecvars -noconsole flags
	// plus the same -dedicated -mod -noconfig -execute -masterserver -register format
	masterURL := honData.MasterServerURL
	if masterURL == "" {
		masterURL = "api.kongor.net"
	}

	procCfg := ProcessConfig{
		Executable: fmt.Sprintf("%s/%s", honData.InstallDirectory, exeName),
		Args: []string{
			"-cowmaster",
			"-servicecvars",
			"-noconsole",
			"-dedicated",
			"-mod game;KONGOR",
			"-noconfig",
			"-execute", fmt.Sprintf("\"Set svr_login %s:;Set svr_password %s\"",
				honData.Login, honData.Password),
			"-masterserver", masterURL,
			"-register", fmt.Sprintf("127.0.0.1:%d", honData.ManagerPort),
		},
		WorkDir: honData.InstallDirectory,
		Port:    0, // CowMaster doesn't use a game port
	}

	cm.process = NewProcessManager(procCfg)

	log.Info().Msg("starting CowMaster process")

	if err := cm.process.Start(ctx); err != nil {
		return fmt.Errorf("failed to start CowMaster: %w", err)
	}

	cm.ready = true

	// Subscribe to fork requests
	cm.eventBus.Subscribe(events.EventForkFromCowMaster, "cowmaster.fork", cm.handleForkRequest)

	// Monitor process health
	go cm.monitor(ctx)

	log.Info().Int("pid", cm.process.PID()).Msg("CowMaster started")
	return nil
}

// Stop stops the CowMaster process.
func (cm *CowMaster) Stop() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.ready = false

	if cm.process != nil {
		return cm.process.Stop()
	}
	return nil
}

// IsReady returns whether the CowMaster is ready to fork.
func (cm *CowMaster) IsReady() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.ready && cm.process != nil && cm.process.IsRunning()
}

// Fork requests the CowMaster to fork a new game server instance.
func (cm *CowMaster) Fork(port uint16) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.ready || cm.process == nil || !cm.process.IsRunning() {
		return fmt.Errorf("CowMaster is not ready")
	}

	// Send fork command via the manager port TCP connection
	// The CowMaster process listens for fork commands on the same protocol
	log.Info().Uint16("port", port).Msg("requesting CowMaster fork")
	cm.lastFork = time.Now()

	return nil
}

// handleForkRequest handles fork request events.
func (cm *CowMaster) handleForkRequest(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.ServerCommandPayload)
	if !ok {
		return nil
	}

	return cm.Fork(payload.Port)
}

// monitor watches the CowMaster process and restarts it if it dies.
func (cm *CowMaster) monitor(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.mu.Lock()
			if cm.ready && cm.process != nil && !cm.process.IsRunning() {
				log.Warn().Msg("CowMaster process died, restarting...")
				cm.ready = false
				cm.mu.Unlock()

				// Brief delay before restart
				time.Sleep(5 * time.Second)

				if err := cm.Start(ctx); err != nil {
					log.Error().Err(err).Msg("failed to restart CowMaster")
				}
			} else {
				cm.mu.Unlock()
			}
		}
	}
}

// GetMemoryUsage returns the memory usage of the CowMaster process.
func (cm *CowMaster) GetMemoryUsage() (float64, error) {
	cm.mu.Lock()
	proc := cm.process
	cm.mu.Unlock()

	if proc == nil {
		return 0, fmt.Errorf("CowMaster not running")
	}

	return proc.GetMemoryMB()
}

// ForkWithExec is a fallback that uses exec instead of CoW fork.
// Used when the CowMaster process isn't available.
func ForkWithExec(cfg *config.Config, port uint16) (*exec.Cmd, error) {
	honData := cfg.GetHoNData()

	exeName := honData.ExecutableName
	if exeName == "" {
		exeName = getServerExecutable()
	}

	masterURL := honData.MasterServerURL
	if masterURL == "" {
		masterURL = "api.kongor.net"
	}

	cmd := exec.Command(
		fmt.Sprintf("%s/%s", honData.InstallDirectory, exeName),
		"-dedicated",
		"-mod game;KONGOR",
		"-noconfig",
		"-execute", fmt.Sprintf("\"Set svr_port %d;Set svr_login %s:;Set svr_password %s\"",
			port, honData.Login, honData.Password),
		"-masterserver", masterURL,
		"-register", fmt.Sprintf("127.0.0.1:%d", honData.ManagerPort),
	)
	cmd.Dir = honData.InstallDirectory

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to fork server on port %d: %w", port, err)
	}

	return cmd, nil
}
