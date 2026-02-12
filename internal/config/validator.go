package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error [%s]: %s", e.Field, e.Message)
}

// ValidationResult holds the results of configuration validation.
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationError
}

// IsValid returns true if there are no validation errors.
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// AddError adds a validation error.
func (r *ValidationResult) AddError(field, message string) {
	r.Errors = append(r.Errors, ValidationError{Field: field, Message: message})
}

// AddWarning adds a validation warning.
func (r *ValidationResult) AddWarning(field, message string) {
	r.Warnings = append(r.Warnings, ValidationError{Field: field, Message: message})
}

// Validate performs comprehensive validation of the configuration.
func Validate(cfg *Config) *ValidationResult {
	result := &ValidationResult{}

	validateHoNData(&cfg.HoNData, result)
	validateApplicationData(&cfg.ApplicationData, result)

	return result
}

func validateHoNData(data *HoNData, result *ValidationResult) {
	// Required fields
	if strings.TrimSpace(data.Login) == "" {
		result.AddError("hon_data.svr_login", "server login is required")
	}

	if strings.TrimSpace(data.Password) == "" {
		result.AddError("hon_data.svr_password", "server password is required")
	}

	if strings.TrimSpace(data.InstallDirectory) == "" {
		result.AddError("hon_data.hon_install_directory", "HoN install directory is required")
	} else if _, err := os.Stat(data.InstallDirectory); os.IsNotExist(err) {
		result.AddWarning("hon_data.hon_install_directory",
			fmt.Sprintf("directory does not exist: %s", data.InstallDirectory))
	}

	// Server count validation
	if data.TotalServers < 1 {
		result.AddError("hon_data.svr_total", "must have at least 1 server")
	}
	if data.TotalServers > 50 {
		result.AddWarning("hon_data.svr_total",
			fmt.Sprintf("high server count (%d) may cause performance issues", data.TotalServers))
	}

	if data.ServersPerCore < 1 {
		result.AddError("hon_data.svr_total_per_core", "must have at least 1 server per core")
	}

	// Port validation
	validatePort(data.StartingGamePort, "hon_data.svr_starting_gamePort", result)
	validatePort(data.StartingVoicePort, "hon_data.svr_starting_voicePort", result)
	validatePort(data.ManagerPort, "hon_data.svr_managerPort", result)
	validatePort(data.APIPort, "hon_data.svr_api_port", result)

	// Port conflict detection
	ports := map[int]string{
		data.StartingGamePort:  "game",
		data.StartingVoicePort: "voice",
		data.ManagerPort:       "manager",
		data.APIPort:           "api",
	}
	if len(ports) < 4 {
		result.AddError("hon_data.ports", "port conflict detected: all ports must be unique")
	}

	// Idle time
	if data.MaxIdleTime < 10 {
		result.AddWarning("hon_data.svr_max_idle_time", "idle time less than 10 seconds may cause issues")
	}
}

func validateApplicationData(data *ApplicationData, result *ValidationResult) {
	// Timer validation
	validateTimers(&data.Timers, result)

	// Replay cleaner
	if data.ReplayCleaner.Enabled {
		if data.ReplayCleaner.RetentionDays < 1 {
			result.AddError("application_data.replay_cleaner.retention_days",
				"retention days must be at least 1")
		}
	}

	// MQTT
	if data.MQTT.Enabled {
		if strings.TrimSpace(data.MQTT.BrokerURL) == "" {
			result.AddError("application_data.mqtt.broker_url", "MQTT broker URL is required when enabled")
		}
		if data.MQTT.Port < 1 || data.MQTT.Port > 65535 {
			result.AddError("application_data.mqtt.port", "invalid MQTT port")
		}
	}

	// Security
	if data.Security.TLSEnabled {
		if strings.TrimSpace(data.Security.TLSCertFile) == "" {
			result.AddError("application_data.security.tls_cert_file",
				"TLS certificate file is required when TLS is enabled")
		}
		if strings.TrimSpace(data.Security.TLSKeyFile) == "" {
			result.AddError("application_data.security.tls_key_file",
				"TLS key file is required when TLS is enabled")
		}
	}

	if data.Security.RateLimitRPS < 1 {
		result.AddWarning("application_data.security.rate_limit_rps",
			"rate limit is disabled (0 RPS), this may expose the API to abuse")
	}

	// Discord
	if data.Discord.OwnerID != "" {
		if len(data.Discord.OwnerID) < 17 || len(data.Discord.OwnerID) > 20 {
			result.AddWarning("application_data.discord.owner_id",
				"Discord owner ID appears invalid (expected 17-20 digit snowflake)")
		}
	}
}

func validateTimers(timers *TimerConfig, result *ValidationResult) {
	if timers.PatchCheckInterval < 30 {
		result.AddWarning("timers.patch_check_interval",
			"patch check interval less than 30s may cause excessive requests")
	}
	if timers.HeartbeatInterval < 10 {
		result.AddWarning("timers.heartbeat_interval",
			"heartbeat interval less than 10s may cause excessive traffic")
	}
}

func validatePort(port int, field string, result *ValidationResult) {
	if port < 1 || port > 65535 {
		result.AddError(field, fmt.Sprintf("invalid port number: %d (must be 1-65535)", port))
		return
	}
	if port < 1024 {
		result.AddWarning(field,
			fmt.Sprintf("port %d is a privileged port, may require elevated permissions", port))
	}
}

// IsPortAvailable checks if a port is available for binding.
func IsPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
