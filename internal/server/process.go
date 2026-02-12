package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/process"
)

// ProcessManager handles the lifecycle of a single game server OS process.
// It wraps os/exec and provides monitoring, CPU affinity, priority control,
// and automatic cleanup capabilities.
type ProcessManager struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	proc   *process.Process
	pid    int
	port   uint16
	logger zerolog.Logger

	// State
	running   bool
	startedAt time.Time
	exitCode  int
	exitErr   error

	// Configuration
	executable   string
	args         []string
	workDir      string
	cpuAffinity  []int32
	highPriority bool
	envVars      map[string]string

	// Platform-specific: Windows process handle from CreateProcessW
	// Used for reliable TerminateProcess during shutdown.
	// On Linux this is unused (zero value).
	processHandle uintptr
}

// ProcessConfig holds configuration for launching a game server process.
type ProcessConfig struct {
	Executable   string
	Args         []string
	WorkDir      string
	Port         uint16
	CPUAffinity  []int32
	HighPriority bool
	EnvVars      map[string]string // Environment variable overrides (USERPROFILE, APPDATA, etc.)
}

// NewProcessManager creates a new process manager for a game server.
func NewProcessManager(cfg ProcessConfig) *ProcessManager {
	return &ProcessManager{
		port:         cfg.Port,
		executable:   cfg.Executable,
		args:         cfg.Args,
		workDir:      cfg.WorkDir,
		cpuAffinity:  cfg.CPUAffinity,
		highPriority: cfg.HighPriority,
		envVars:      cfg.EnvVars,
		logger: log.With().
			Str("component", "process").
			Uint16("port", cfg.Port).
			Logger(),
	}
}

// Start launches the game server process.
// On Windows: uses CreateProcessW directly (bypassing Go's exec wrapper) to ensure
// the command line is passed exactly as Python's subprocess.Popen would.
// On Linux: uses exec.CommandContext with platform-specific process attributes.
func (pm *ProcessManager) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return fmt.Errorf("process already running (pid: %d)", pm.pid)
	}

	pm.logger.Info().
		Str("executable", pm.executable).
		Strs("args", pm.args).
		Str("workdir", pm.workDir).
		Msg("starting game server process")

	return pm.startPlatform(ctx)
}

// monitorDirect monitors a directly-created Windows process using gopsutil.
// This replaces monitor() for processes started without exec.Cmd.
//
// When the context is cancelled (e.g. application shutdown) the monitor stops
// polling but does NOT terminate the game server — explicit Stop() or Kill()
// should be used for that. This prevents HTTP-request-scoped contexts from
// accidentally killing the game server as soon as the request completes.
func (pm *ProcessManager) monitorDirect(_ context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		pm.mu.Lock()
		proc := pm.proc
		pid := pm.pid
		isRunning := pm.running
		pm.mu.Unlock()

		if !isRunning || proc == nil {
			return
		}

		running, err := proc.IsRunning()
		if err != nil || !running {
			pm.mu.Lock()
			pm.running = false
			pm.exitCode = -1 // Unknown exit code for direct process
			pm.mu.Unlock()

			pm.logger.Info().
				Int("pid", pid).
				Msg("game server process exited")
			return
		}
	}
}

// Stop gracefully stops the game server process.
// On Windows: uses taskkill for reliable termination.
// On Linux: sends SIGTERM first, then SIGKILL after timeout.
func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return nil
	}

	pm.logger.Info().Int("pid", pm.pid).Msg("stopping game server process")

	if runtime.GOOS == "windows" {
		// Windows: use stored process handle for reliable TerminateProcess
		if pm.processHandle != 0 {
			if err := terminateProcessWithHandle(syscallHandle(pm.processHandle)); err != nil {
				pm.logger.Warn().Err(err).Msg("TerminateProcess failed, trying taskkill")
				terminateProcessPlatform(pm.pid)
			}
		} else {
			terminateProcessPlatform(pm.pid)
		}
		pm.running = false
		return nil
	}

	// Linux: Try SIGTERM first for graceful shutdown
	if pm.cmd == nil || pm.cmd.Process == nil {
		pm.running = false
		return nil
	}

	if err := pm.cmd.Process.Signal(os.Interrupt); err != nil {
		pm.logger.Warn().Err(err).Msg("graceful shutdown failed, force killing")
		pm.running = false
		return pm.cmd.Process.Kill()
	}

	// Give it 10 seconds to shut down gracefully
	done := make(chan error, 1)
	go func() {
		done <- pm.cmd.Wait()
	}()

	select {
	case <-done:
		pm.logger.Info().Msg("process stopped gracefully")
	case <-time.After(10 * time.Second):
		pm.logger.Warn().Msg("process didn't stop in 10s, force killing")
		pm.cmd.Process.Kill()
	}

	pm.running = false
	return nil
}

// Kill immediately terminates the game server process.
func (pm *ProcessManager) Kill() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return nil
	}

	pm.logger.Warn().Int("pid", pm.pid).Msg("force killing game server process")

	if runtime.GOOS == "windows" {
		if pm.processHandle != 0 {
			terminateProcessWithHandle(syscallHandle(pm.processHandle))
		} else {
			terminateProcessPlatform(pm.pid)
		}
		pm.running = false
		return nil
	}

	if pm.cmd != nil && pm.cmd.Process != nil {
		err := pm.cmd.Process.Kill()
		pm.running = false
		return err
	}

	pm.running = false
	return nil
}

// IsRunning returns whether the process is currently running.
func (pm *ProcessManager) IsRunning() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.running
}

// PID returns the process ID.
func (pm *ProcessManager) PID() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.pid
}

// StartedAt returns when the process was started.
func (pm *ProcessManager) StartedAt() time.Time {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.startedAt
}

// Uptime returns how long the process has been running.
func (pm *ProcessManager) Uptime() time.Duration {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if !pm.running {
		return 0
	}
	return time.Since(pm.startedAt)
}

// ExitCode returns the exit code of the process (-1 if still running).
func (pm *ProcessManager) ExitCode() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.exitCode
}

// GetCPUPercent returns the CPU usage percentage of the process.
func (pm *ProcessManager) GetCPUPercent() (float64, error) {
	pm.mu.Lock()
	proc := pm.proc
	pm.mu.Unlock()

	if proc == nil {
		return 0, fmt.Errorf("process not available")
	}
	return proc.CPUPercent()
}

// GetMemoryMB returns the memory usage in megabytes.
func (pm *ProcessManager) GetMemoryMB() (float64, error) {
	pm.mu.Lock()
	proc := pm.proc
	pm.mu.Unlock()

	if proc == nil {
		return 0, fmt.Errorf("process not available")
	}

	memInfo, err := proc.MemoryInfo()
	if err != nil {
		return 0, err
	}

	return float64(memInfo.RSS) / (1024 * 1024), nil
}

// SetHighPriority increases the process priority.
func (pm *ProcessManager) SetHighPriority() error {
	pm.mu.Lock()
	proc := pm.proc
	pm.mu.Unlock()

	if proc == nil {
		return fmt.Errorf("process not available")
	}

	// Platform-specific priority setting
	if runtime.GOOS == "windows" {
		// Windows: HIGH_PRIORITY_CLASS
		return setWindowsPriority(pm.pid, true)
	}
	// Linux: nice -5 (higher priority)
	nice, err := proc.Nice()
	if err != nil {
		return err
	}
	_ = nice
	// gopsutil doesn't support SetNice directly; use OS-level call
	return nil
}

// SetNormalPriority restores normal process priority.
func (pm *ProcessManager) SetNormalPriority() error {
	pm.mu.Lock()
	proc := pm.proc
	pm.mu.Unlock()

	if proc == nil {
		return fmt.Errorf("process not available")
	}

	if runtime.GOOS == "windows" {
		return setWindowsPriority(pm.pid, false)
	}
	// Restore normal priority on Linux
	return nil
}

// setCPUAffinity pins the process to specific CPU cores.
func (pm *ProcessManager) setCPUAffinity() {
	// Give the process a moment to start
	time.Sleep(500 * time.Millisecond)

	pm.mu.Lock()
	proc := pm.proc
	affinity := pm.cpuAffinity
	pm.mu.Unlock()

	if proc == nil || len(affinity) == 0 {
		return
	}

	if err := setCPUAffinityPlatform(pm.pid, affinity); err != nil {
		pm.logger.Warn().
			Err(err).
			Ints32("cores", affinity).
			Msg("failed to set CPU affinity")
	} else {
		pm.logger.Debug().
			Ints32("cores", affinity).
			Msg("CPU affinity set")
	}
}

// monitor watches the process and updates state when it exits.
func (pm *ProcessManager) monitor() {
	if pm.cmd == nil {
		return
	}

	err := pm.cmd.Wait()

	pm.mu.Lock()
	pm.running = false
	pm.exitErr = err
	if pm.cmd.ProcessState != nil {
		pm.exitCode = pm.cmd.ProcessState.ExitCode()
	}
	pid := pm.pid
	exitCode := pm.exitCode
	pm.mu.Unlock()

	pm.logger.Info().
		Int("pid", pid).
		Int("exit_code", exitCode).
		Msg("game server process exited")
}

// NOTE: The following functions are implemented in platform-specific files:
//   setPlatformProcessAttrs(cmd *exec.Cmd)            → process_windows.go / process_linux.go
//   setWindowsPriority(pid int, high bool) error       → process_windows.go / process_linux.go
//   setCPUAffinityPlatform(pid int, cores []int32)     → process_windows.go / process_linux.go
//   terminateProcessPlatform(pid int)                  → process_windows.go / process_linux.go
