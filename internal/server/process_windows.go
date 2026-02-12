//go:build windows

package server

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/shirou/gopsutil/v3/process"
)

// Windows process creation flags
const (
	_DETACHED_PROCESS            = 0x00000008
	_CREATE_NEW_CONSOLE          = 0x00000010
	_HIGH_PRIORITY_CLASS         = 0x00000080
	_IDLE_PRIORITY_CLASS         = 0x00000040
	_NORMAL_PRIORITY_CLASS       = 0x00000020
	_CREATE_UNICODE_ENVIRONMENT  = 0x00000400
	_PROCESS_SET_INFORMATION     = 0x0200
	_PROCESS_QUERY_INFORMATION   = 0x0400
)

var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	procSetPriorityClass       = kernel32.NewProc("SetPriorityClass")
	procSetProcessAffinityMask = kernel32.NewProc("SetProcessAffinityMask")
)

// startPlatform launches the game server using Windows CreateProcessW directly.
// This matches Python's subprocess.Popen behavior exactly:
// - lpApplicationName = nil (executable parsed from command line)
// - lpCommandLine = full command line with proper escaping
// - bInheritHandles = false (matching close_fds=True)
// - CREATE_NEW_CONSOLE for visible console window
func (pm *ProcessManager) startPlatform(ctx context.Context) error {
	// Log the exact Windows command line for debugging
	cmdLine := buildWindowsCmdLine(pm.executable, pm.args)
	pm.logger.Info().
		Str("cmdline", cmdLine).
		Msg("Windows CreateProcess command line")

	pid, handle, err := startProcessDirect(pm.executable, pm.args, pm.workDir, pm.envVars)
	if err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	pm.pid = pid
	pm.processHandle = uintptr(handle) // Store handle for TerminateProcess
	pm.running = true
	pm.startedAt = time.Now()
	pm.exitCode = -1
	pm.exitErr = nil
	pm.cmd = nil // No exec.Cmd when using direct launch

	pm.logger.Info().
		Int("pid", pm.pid).
		Msg("game server process started (direct CreateProcess)")

	// Get gopsutil process handle for monitoring
	if p, err := process.NewProcess(int32(pm.pid)); err == nil {
		pm.proc = p
	}

	// Set CPU affinity if configured
	if len(pm.cpuAffinity) > 0 {
		go pm.setCPUAffinity()
	}

	// Monitor the process using gopsutil (since we don't have exec.Cmd)
	go pm.monitorDirect(ctx)

	return nil
}

// syscallHandle converts a uintptr to syscall.Handle.
// Used to store the Windows process handle in the platform-independent ProcessManager.
func syscallHandle(h uintptr) syscall.Handle {
	return syscall.Handle(h)
}

// closeProcessHandle closes a stored Windows process handle.
func closeProcessHandle(h uintptr) {
	if h != 0 {
		syscall.CloseHandle(syscall.Handle(h))
	}
}

// setPlatformProcessAttrs sets Windows-specific process creation flags.
// This is kept for fallback but the primary launch method is startProcessDirect.
func setPlatformProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: _CREATE_NEW_CONSOLE,
	}
}

// startProcessDirect launches a process using Windows CreateProcessW directly.
// This bypasses Go's exec.Command wrapper to match exactly how Python's
// subprocess.Popen(cmdline, close_fds=True, creationflags=DETACHED_PROCESS)
// constructs the Windows command line.
//
// Returns the PID and the process HANDLE of the launched process.
// The caller MUST keep the handle open and use it for TerminateProcess.
// Close the handle with syscall.CloseHandle when done.
func startProcessDirect(executable string, args []string, workDir string, envOverrides map[string]string) (int, syscall.Handle, error) {
	// Build command line string exactly like Python's subprocess.list2cmdline
	cmdLine := buildWindowsCmdLine(executable, args)

	// Build environment block with overrides
	envBlock := buildEnvBlock(envOverrides)

	var si syscall.StartupInfo
	var pi syscall.ProcessInformation
	si.Cb = uint32(unsafe.Sizeof(si))

	// Convert strings to UTF16
	cmdLinePtr, err := syscall.UTF16PtrFromString(cmdLine)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to encode command line: %w", err)
	}

	workDirPtr, err := syscall.UTF16PtrFromString(workDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to encode work dir: %w", err)
	}

	// Determine creation flags
	// CREATE_NEW_CONSOLE: visible console window for the game server
	// CREATE_UNICODE_ENVIRONMENT: required when passing a UTF-16 environment block
	flags := uint32(_CREATE_NEW_CONSOLE)
	if envBlock != nil {
		flags |= _CREATE_UNICODE_ENVIRONMENT
	}

	// CreateProcessW with:
	// - lpApplicationName = nil (executable is in cmdLine)
	// - lpCommandLine = full command line
	// - bInheritHandles = false (close_fds=True equivalent)
	err = syscall.CreateProcess(
		nil,           // lpApplicationName (nil = parse from cmdLine)
		cmdLinePtr,    // lpCommandLine
		nil,           // lpProcessAttributes
		nil,           // lpThreadAttributes
		false,         // bInheritHandles (= close_fds=True)
		flags,         // dwCreationFlags
		envBlock,      // lpEnvironment (nil = inherit, or custom block)
		workDirPtr,    // lpCurrentDirectory
		&si,           // lpStartupInfo
		&pi,           // lpProcessInformation
	)
	if err != nil {
		return 0, 0, fmt.Errorf("CreateProcess failed: %w", err)
	}

	// Close the thread handle (not needed), but KEEP the process handle
	// for reliable TerminateProcess during shutdown.
	syscall.CloseHandle(pi.Thread)

	return int(pi.ProcessId), pi.Process, nil
}

// buildWindowsCmdLine constructs a Windows command line string from executable and args.
// This matches Python's subprocess.list2cmdline() behavior:
// - Arguments with spaces, tabs, or double quotes get wrapped in double quotes
// - Backslashes before double quotes get escaped
// - The executable path (if it has spaces) gets quoted
func buildWindowsCmdLine(executable string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, escapeArg(executable))
	for _, arg := range args {
		parts = append(parts, escapeArg(arg))
	}
	return strings.Join(parts, " ")
}

// escapeArg escapes a single argument for Windows command line.
// Matches Python's subprocess.list2cmdline logic.
func escapeArg(s string) string {
	if s == "" {
		return `""`
	}
	// Check if quoting is needed
	needsQuote := false
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '"' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}

	// Build quoted string
	var buf strings.Builder
	buf.WriteByte('"')
	nbs := 0 // number of pending backslashes
	for _, c := range s {
		switch c {
		case '\\':
			nbs++
		case '"':
			// Escape backslashes before quote, then escape the quote
			for i := 0; i < nbs; i++ {
				buf.WriteByte('\\')
			}
			nbs = 0
			buf.WriteString(`\"`)
		default:
			// Write pending backslashes (unescaped)
			for i := 0; i < nbs; i++ {
				buf.WriteByte('\\')
			}
			nbs = 0
			buf.WriteRune(c)
		}
	}
	// Escape trailing backslashes before closing quote
	for i := 0; i < nbs; i++ {
		buf.WriteByte('\\')
	}
	buf.WriteByte('"')
	return buf.String()
}

// buildEnvBlock creates a Windows environment block (double-null-terminated UTF16)
// with the specified overrides applied to the current environment.
// Returns nil to inherit the parent environment if no overrides are needed.
//
// Windows environment blocks are structured as:
//
//	"KEY1=VALUE1\0KEY2=VALUE2\0\0"
//
// Each entry is null-terminated, and the entire block ends with an extra null.
// Must be encoded as UTF-16 for CreateProcessW.
func buildEnvBlock(overrides map[string]string) *uint16 {
	if len(overrides) == 0 {
		return nil // Inherit parent environment
	}

	// Build map from current environment
	envMap := make(map[string]string)
	for _, e := range syscall.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[strings.ToUpper(parts[0])] = e
		}
	}

	// Apply overrides
	for k, v := range overrides {
		envMap[strings.ToUpper(k)] = fmt.Sprintf("%s=%s", k, v)
	}

	// Build the environment block as a UTF-16 slice directly.
	// We can't use syscall.UTF16FromString because it rejects embedded nulls.
	var block []uint16
	for _, v := range envMap {
		// Encode each "KEY=VALUE" entry followed by a null terminator
		encoded := utf16.Encode([]rune(v))
		block = append(block, encoded...)
		block = append(block, 0) // null terminator for this entry
	}
	block = append(block, 0) // final double-null terminator

	return &block[0]
}

// setWindowsPriority sets the process priority class on Windows.
// HoNfigurator uses psutil.IDLE_PRIORITY_CLASS at start, then
// psutil.REALTIME_PRIORITY_CLASS when players connect.
func setWindowsPriority(pid int, high bool) error {
	handle, err := syscall.OpenProcess(_PROCESS_SET_INFORMATION, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer syscall.CloseHandle(handle)

	var priority uintptr
	if high {
		priority = _HIGH_PRIORITY_CLASS
	} else {
		priority = _NORMAL_PRIORITY_CLASS
	}

	ret, _, callErr := procSetPriorityClass.Call(uintptr(handle), priority)
	if ret == 0 {
		return fmt.Errorf("SetPriorityClass(%d): %w", pid, callErr)
	}
	return nil
}

var procTerminateProcess = kernel32.NewProc("TerminateProcess")

// terminateProcessWithHandle uses the stored process HANDLE from CreateProcessW
// to call TerminateProcess directly. This is the most reliable way to kill a process
// because we own the handle — no need for taskkill or OpenProcess.
func terminateProcessWithHandle(handle syscall.Handle) error {
	if handle == 0 {
		return fmt.Errorf("invalid process handle")
	}
	ret, _, err := procTerminateProcess.Call(uintptr(handle), 0)
	if ret == 0 {
		return fmt.Errorf("TerminateProcess: %w", err)
	}
	return nil
}

// terminateProcessPlatform forcefully terminates a process on Windows by PID.
// Fallback method when we don't have a process handle.
func terminateProcessPlatform(pid int) {
	// taskkill /F /T /PID <pid> → force kill + kill child processes
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	cmd.Run()
}

// setCPUAffinityPlatform sets CPU affinity for a process on Windows.
// Mirrors HoNfigurator-Central: psutil.Process(pid).cpu_affinity([...])
func setCPUAffinityPlatform(pid int, cores []int32) error {
	handle, err := syscall.OpenProcess(_PROCESS_SET_INFORMATION|_PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer syscall.CloseHandle(handle)

	var mask uintptr
	for _, core := range cores {
		if core >= 0 && core < 64 {
			mask |= 1 << uint(core)
		}
	}

	if mask == 0 {
		return nil // No valid cores specified
	}

	ret, _, callErr := procSetProcessAffinityMask.Call(uintptr(handle), mask)
	if ret == 0 {
		return fmt.Errorf("SetProcessAffinityMask(%d): %w", pid, callErr)
	}
	return nil
}
