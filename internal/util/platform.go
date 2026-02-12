package util

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// Platform represents the current operating system.
type Platform string

const (
	PlatformWindows Platform = "windows"
	PlatformLinux   Platform = "linux"
	PlatformUnknown Platform = "unknown"
)

// GetPlatform returns the current platform.
func GetPlatform() Platform {
	switch runtime.GOOS {
	case "windows":
		return PlatformWindows
	case "linux":
		return PlatformLinux
	default:
		return PlatformUnknown
	}
}

// IsWindows returns true if running on Windows.
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// IsLinux returns true if running on Linux.
func IsLinux() bool {
	return runtime.GOOS == "linux"
}

// SystemInfo holds information about the host system.
type SystemInfo struct {
	Platform     Platform `json:"platform"`
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	CPUModel     string   `json:"cpu_model"`
	CPUCores     int      `json:"cpu_cores"`
	CPUThreads   int      `json:"cpu_threads"`
	TotalMemory  uint64   `json:"total_memory_mb"`
}

// GetSystemInfo gathers system information.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Platform:     GetPlatform(),
		Architecture: runtime.GOARCH,
		CPUCores:     runtime.NumCPU(),
	}

	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}

	if hostInfo, err := host.Info(); err == nil {
		info.OS = fmt.Sprintf("%s %s", hostInfo.Platform, hostInfo.PlatformVersion)
	}

	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info.CPUModel = cpuInfo[0].ModelName
		info.CPUThreads = int(cpuInfo[0].Cores)
	}

	if memInfo, err := mem.VirtualMemory(); err == nil {
		info.TotalMemory = memInfo.Total / (1024 * 1024) // Convert to MB
	}

	return info
}

// GetPublicIP detects the public IP address of this machine.
func GetPublicIP() (string, error) {
	// Try to determine public IP by connecting to a known external server
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("failed to detect public IP: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// GetLocalIP returns the primary local IP address.
func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String(), nil
			}
		}
	}
	return "127.0.0.1", nil
}

// DiskUsage returns disk usage statistics for the given path.
type DiskUsage struct {
	Total       uint64  `json:"total_gb"`
	Used        uint64  `json:"used_gb"`
	Free        uint64  `json:"free_gb"`
	UsedPercent float64 `json:"used_percent"`
}

// GetDiskUsage returns disk usage for the specified path.
func GetDiskUsage(path string) (*DiskUsage, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return nil, err
	}

	return &DiskUsage{
		Total:       usage.Total / (1024 * 1024 * 1024),
		Used:        usage.Used / (1024 * 1024 * 1024),
		Free:        usage.Free / (1024 * 1024 * 1024),
		UsedPercent: usage.UsedPercent,
	}, nil
}

// GetCPUUsage returns the current CPU usage percentage.
func GetCPUUsage() (float64, error) {
	percentages, err := cpu.Percent(0, false)
	if err != nil {
		return 0, err
	}
	if len(percentages) > 0 {
		return percentages[0], nil
	}
	return 0, nil
}

// GetMemoryUsage returns current memory usage.
type MemoryUsage struct {
	Total       uint64  `json:"total_mb"`
	Used        uint64  `json:"used_mb"`
	Available   uint64  `json:"available_mb"`
	UsedPercent float64 `json:"used_percent"`
}

// GetMemoryUsage returns current system memory usage.
func GetMemoryUsage() (*MemoryUsage, error) {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &MemoryUsage{
		Total:       memInfo.Total / (1024 * 1024),
		Used:        memInfo.Used / (1024 * 1024),
		Available:   memInfo.Available / (1024 * 1024),
		UsedPercent: memInfo.UsedPercent,
	}, nil
}

// IsProcessRunning checks if a process with the given PID is running.
func IsProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds; we need to signal 0 to check
	if !IsWindows() {
		err = process.Signal(os.Signal(nil))
		return err == nil
	}

	// On Windows, FindProcess succeeds only if process exists
	return process != nil
}

// ExecuteCommand runs a system command and returns its output.
func ExecuteCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command '%s %s' failed: %w (output: %s)",
			name, strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// FileExists checks if a file or directory exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// EnsureDir creates a directory and all parent directories if they don't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// KillProcess terminates a process by PID.
func KillProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	if err := process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	log.Info().Int("pid", pid).Msg("process killed")
	return nil
}
