//go:build linux

package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v3/process"
)

// startPlatform launches the game server using Go's exec.Command on Linux.
// We intentionally do NOT use exec.CommandContext here because that would
// kill the process when the parent context is cancelled (e.g. HTTP request
// completing). Process termination is handled explicitly by Stop()/Kill().
func (pm *ProcessManager) startPlatform(_ context.Context) error {
	pm.cmd = exec.Command(pm.executable, pm.args...)
	pm.cmd.Dir = pm.workDir

	// Override environment variables for per-instance file isolation.
	if len(pm.envVars) > 0 {
		baseEnv := os.Environ()
		overrideKeys := make(map[string]bool)
		for k := range pm.envVars {
			overrideKeys[strings.ToUpper(k)] = true
		}
		var env []string
		for _, e := range baseEnv {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 && overrideKeys[strings.ToUpper(parts[0])] {
				continue
			}
			env = append(env, e)
		}
		for k, v := range pm.envVars {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		pm.cmd.Env = env
	}

	setPlatformProcessAttrs(pm.cmd)

	if err := pm.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	pm.pid = pm.cmd.Process.Pid
	pm.running = true
	pm.startedAt = time.Now()
	pm.exitCode = -1
	pm.exitErr = nil

	pm.logger.Info().
		Int("pid", pm.pid).
		Msg("game server process started")

	if p, err := process.NewProcess(int32(pm.pid)); err == nil {
		pm.proc = p
	}

	if len(pm.cpuAffinity) > 0 {
		go pm.setCPUAffinity()
	}

	go pm.monitor()

	return nil
}

// syscallHandle is a no-op on Linux (returns 0).
func syscallHandle(h uintptr) uintptr { return h }

// closeProcessHandle is a no-op on Linux.
func closeProcessHandle(h uintptr) {}

// terminateProcessWithHandle is a no-op on Linux (uses SIGKILL via PID).
func terminateProcessWithHandle(handle uintptr) error {
	return fmt.Errorf("not supported on linux")
}

// setPlatformProcessAttrs sets Linux-specific process creation attributes.
// Mirrors HoNfigurator-Central: subprocess.Popen(cmdline, close_fds=True,
//
//	start_new_session=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
func setPlatformProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Equivalent to start_new_session=True
	}
	// Redirect stdout/stderr to /dev/null (matches subprocess.DEVNULL)
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		cmd.Stdout = devnull
		cmd.Stderr = devnull
	}
}

// setWindowsPriority is a no-op on Linux.
func setWindowsPriority(pid int, high bool) error {
	return nil
}

// terminateProcessPlatform sends SIGKILL to the process on Linux.
func terminateProcessPlatform(pid int) {
	syscall.Kill(pid, syscall.SIGKILL)
}

// setCPUAffinityPlatform sets CPU affinity for a process on Linux.
// Uses sched_setaffinity syscall, mirroring psutil.Process.cpu_affinity().
func setCPUAffinityPlatform(pid int, cores []int32) error {
	if len(cores) == 0 {
		return nil
	}

	// Build CPU set bitmask (supports up to 1024 CPUs)
	const cpuSetSize = 128 // 128 * 8 bytes = 1024 bits
	var cpuSet [cpuSetSize]uint64

	for _, core := range cores {
		if core >= 0 && core < 1024 {
			idx := core / 64
			bit := uint(core % 64)
			cpuSet[idx] |= 1 << bit
		}
	}

	_, _, errno := syscall.RawSyscall(
		syscall.SYS_SCHED_SETAFFINITY,
		uintptr(pid),
		uintptr(len(cpuSet)*8),
		uintptr(unsafe.Pointer(&cpuSet[0])),
	)
	if errno != 0 {
		return fmt.Errorf("sched_setaffinity(%d): %v", pid, errno)
	}
	return nil
}
