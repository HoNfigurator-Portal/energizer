package config

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// RunSetupWizard guides the user through first-time configuration.
func RunSetupWizard(cfg *Config) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         Energizer - First Run Setup          ║")
	fmt.Println("╠══════════════════════════════════════════════╣")
	fmt.Println("║  Welcome! Let's configure your server.       ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	// Terms and conditions
	fmt.Println("By using Energizer, you agree to:")
	fmt.Println("  1. Submit match logs to upstream services")
	fmt.Println("  2. Accept the risks of hosting game servers")
	fmt.Println("  3. Your public IP will be exposed to players")
	fmt.Println()

	accept := promptString(reader, "Do you accept these terms? (yes/no)", "no")
	if strings.ToLower(accept) != "yes" {
		return fmt.Errorf("terms not accepted, cannot continue")
	}

	fmt.Println()
	fmt.Println("── Server Credentials ──")

	cfg.HoNData.Login = promptString(reader, "Project Kongor username", cfg.HoNData.Login)
	cfg.HoNData.Password = promptPassword(reader, "Project Kongor password")

	fmt.Println()
	fmt.Println("── Server Identity ──")

	cfg.HoNData.Name = promptString(reader, "Server name (e.g. KONGOR ARENA)", cfg.HoNData.Name)
	cfg.HoNData.Location = promptString(reader, "Server location code (e.g. USE, USW, EU, SEA)", cfg.HoNData.Location)
	cfg.HoNData.IP = promptString(reader, "Public IP address (leave blank to auto-detect)", cfg.HoNData.IP)

	fmt.Println()
	fmt.Println("── Server Paths ──")

	defaultInstallDir := getDefaultInstallDir()
	cfg.HoNData.InstallDirectory = promptString(reader, "HoN install directory", defaultInstallDir)
	cfg.HoNData.HomeDirectory = promptString(reader, "HoN home/config directory",
		cfg.HoNData.InstallDirectory)
	cfg.HoNData.ArtefactsDirectory = promptString(reader, "HoN artefacts directory (replays/logs)",
		cfg.HoNData.HomeDirectory)

	fmt.Println()
	fmt.Println("── Server Pool ──")

	cfg.HoNData.TotalServers = promptInt(reader, "Number of game servers", cfg.HoNData.TotalServers)
	cfg.HoNData.ServersPerCore = promptInt(reader, "Servers per CPU core", cfg.HoNData.ServersPerCore)

	fmt.Println()
	fmt.Println("── Network Ports ──")

	cfg.HoNData.StartingGamePort = promptInt(reader, "Starting game port", cfg.HoNData.StartingGamePort)
	cfg.HoNData.StartingVoicePort = promptInt(reader, "Starting voice port", cfg.HoNData.StartingVoicePort)
	cfg.HoNData.ManagerPort = promptInt(reader, "Manager port (TCP listener)", cfg.HoNData.ManagerPort)
	cfg.HoNData.APIPort = promptInt(reader, "REST API port", cfg.HoNData.APIPort)

	fmt.Println()
	fmt.Println("── Platform Features ──")

	if runtime.GOOS == "windows" {
		cfg.HoNData.EnableProxy = promptBool(reader, "Enable proxy mode", cfg.HoNData.EnableProxy)
	} else {
		cfg.HoNData.UseCowMaster = promptBool(reader, "Enable CowMaster (fork mode)", cfg.HoNData.UseCowMaster)
	}

	fmt.Println()
	fmt.Println("── Discord Integration ──")

	cfg.ApplicationData.Discord.OwnerID = promptString(reader,
		"Discord owner ID (for notifications)", cfg.ApplicationData.Discord.OwnerID)

	fmt.Println()
	fmt.Println("── MQTT Telemetry ──")

	cfg.ApplicationData.MQTT.Enabled = promptBool(reader, "Enable MQTT telemetry", cfg.ApplicationData.MQTT.Enabled)

	// Validate before saving
	result := Validate(cfg)
	if !result.IsValid() {
		fmt.Println("\n⚠ Configuration has errors:")
		for _, e := range result.Errors {
			fmt.Printf("  - [%s] %s\n", e.Field, e.Message)
		}
		retry := promptString(reader, "Would you like to try again? (yes/no)", "yes")
		if strings.ToLower(retry) == "yes" {
			return RunSetupWizard(cfg)
		}
		return fmt.Errorf("configuration validation failed")
	}

	for _, w := range result.Warnings {
		log.Warn().Str("field", w.Field).Msg(w.Message)
	}

	// Save configuration
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Configuration saved successfully!")
	fmt.Println("  Energizer will now start with your configuration.")
	fmt.Println()

	return nil
}

func promptString(reader *bufio.Reader, prompt string, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("  %s: ", prompt)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultVal
	}
	return input
}

func promptPassword(reader *bufio.Reader, prompt string) string {
	fmt.Printf("  %s: ", prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func promptInt(reader *bufio.Reader, prompt string, defaultVal int) int {
	fmt.Printf("  %s [%d]: ", prompt, defaultVal)

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultVal
	}

	val, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("    Invalid number, using default: %d\n", defaultVal)
		return defaultVal
	}
	return val
}

func promptBool(reader *bufio.Reader, prompt string, defaultVal bool) bool {
	defaultStr := "no"
	if defaultVal {
		defaultStr = "yes"
	}

	fmt.Printf("  %s [%s]: ", prompt, defaultStr)

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultVal
	}

	return input == "yes" || input == "y" || input == "true" || input == "1"
}

func getDefaultInstallDir() string {
	if runtime.GOOS == "windows" {
		return "C:\\Program Files\\Heroes of Newerth"
	}
	return "/opt/hon/app"
}
