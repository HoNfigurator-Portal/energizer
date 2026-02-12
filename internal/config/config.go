// Package config handles configuration loading, validation, and persistence
// for the Energizer server manager.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
)

const (
	DefaultConfigDir  = "config"
	DefaultConfigFile = "config.json"
	DefaultAPIPort    = 5000
	DefaultManagerPort = 1134
	DefaultGamePort   = 11235
	DefaultVoicePort  = 11335
)

// Config is the root configuration structure for Energizer.
type Config struct {
	mu   sync.RWMutex
	path string

	HoNData         HoNData         `json:"hon_data"`
	ApplicationData ApplicationData `json:"application_data"`
}

// HoNData contains game server specific configuration.
type HoNData struct {
	// Paths
	InstallDirectory   string `json:"hon_install_directory"`
	HomeDirectory      string `json:"hon_home_directory"`
	ArtefactsDirectory string `json:"hon_artefacts_directory"`

	// Executable
	ExecutableName string `json:"hon_executable_name"`

	// Server credentials
	Login    string `json:"svr_login"`
	Password string `json:"svr_password"`

	// Server identity
	Name     string `json:"svr_name"`
	Location string `json:"svr_location"`
	Region   string `json:"svr_region"`
	IP       string `json:"svr_ip"`

	// Server pool
	TotalServers   int `json:"svr_total"`
	ServersPerCore int `json:"svr_total_per_core"`

	// Ports
	StartingGamePort           int `json:"svr_starting_gamePort"`
	StartingVoicePort          int `json:"svr_starting_voicePort"`
	ManagerPort                int `json:"svr_managerPort"`
	APIPort                    int `json:"svr_api_port"`
	ProxyStartPort             int `json:"svr_starting_proxyPort"`
	VoiceProxyLocalStartPort   int `json:"svr_starting_voiceProxyLocalPort"`
	VoiceProxyRemoteStartPort  int `json:"svr_starting_voiceProxyRemotePort"`

	// Upstream
	MasterServerURL string `json:"svr_masterServer"`
	ChatAddress     string `json:"svr_chatAddress"`
	ChatPort        int    `json:"svr_chatPort"`

	// Features
	EnableProxy      bool `json:"man_enableProxy"`
	UseCowMaster     bool `json:"man_use_cowmaster"`
	BetaMode         bool `json:"svr_beta_mode"`
	NoConsole        bool `json:"svr_noConsole"`
	OverrideAffinity bool `json:"svr_override_affinity"`

	// Game settings
	AllowBotMatches bool   `json:"svr_allow_bot_matches"`
	MaxIdleTime     int    `json:"svr_max_idle_time"`
	ServerVersion   string `json:"svr_version"`
}

// ApplicationData contains manager application configuration.
type ApplicationData struct {
	Timers          TimerConfig          `json:"timers"`
	ReplayCleaner   ReplayCleanerConfig  `json:"replay_cleaner"`
	LongTermStorage LongTermStorageConfig `json:"longterm_storage"`
	Filebeat        FilebeatConfig       `json:"filebeat"`
	Discord         DiscordConfig        `json:"discord"`
	MQTT            MQTTConfig           `json:"mqtt"`
	Security        SecurityConfig       `json:"security"`
	Logging         LoggingConfig        `json:"logging"`
}

// TimerConfig holds health check and task interval settings.
type TimerConfig struct {
	PatchCheckInterval       int `json:"patch_check_interval_sec"`
	VersionCheckInterval     int `json:"version_check_interval_sec"`
	PublicIPCheckInterval    int `json:"public_ip_check_interval_sec"`
	GeneralHealthInterval    int `json:"general_health_interval_sec"`
	DiskCheckInterval        int `json:"disk_check_interval_sec"`
	LagCheckInterval         int `json:"lag_check_interval_sec"`
	FilebeatCheckInterval    int `json:"filebeat_check_interval_sec"`
	AutopingCheckInterval    int `json:"autoping_check_interval_sec"`
	StatsPollingInterval     int `json:"stats_polling_interval_sec"`
	HeartbeatInterval        int `json:"heartbeat_interval_sec"`
	TaskCleanupInterval      int `json:"task_cleanup_interval_sec"`
}

// ReplayCleanerConfig holds replay cleanup settings.
type ReplayCleanerConfig struct {
	Enabled          bool   `json:"enabled"`
	CleanupTime      string `json:"cleanup_time"`
	RetentionDays    int    `json:"retention_days"`
	TmpRetentionDays int    `json:"tmp_retention_days"`
}

// LongTermStorageConfig holds offsite replay storage settings.
type LongTermStorageConfig struct {
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
}

// FilebeatConfig holds Filebeat integration settings.
type FilebeatConfig struct {
	Enabled bool `json:"enabled"`
}

// DiscordConfig holds Discord integration settings.
type DiscordConfig struct {
	OwnerID       string `json:"owner_id"`
	WebhookURL    string `json:"webhook_url"`
	NotifyOnLag   bool   `json:"notify_on_lag"`
	NotifyOnCrash bool   `json:"notify_on_crash"`
	NotifyOnDisk  bool   `json:"notify_on_disk"`
}

// MQTTConfig holds MQTT telemetry settings.
type MQTTConfig struct {
	Enabled    bool   `json:"enabled"`
	BrokerURL  string `json:"broker_url"`
	Port       int    `json:"port"`
	UseTLS     bool   `json:"use_tls"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file"`
	ClientID   string `json:"client_id"`
}

// SecurityConfig holds security-related settings.
type SecurityConfig struct {
	TLSEnabled     bool     `json:"tls_enabled"`
	TLSCertFile    string   `json:"tls_cert_file"`
	TLSKeyFile     string   `json:"tls_key_file"`
	AllowedOrigins []string `json:"allowed_origins"`
	RateLimitRPS   int      `json:"rate_limit_rps"`
	IPWhitelist    []string `json:"ip_whitelist"`
	AuthDisabled   bool     `json:"auth_disabled"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level      string `json:"level"`
	Directory  string `json:"directory"`
	MaxSizeMB  int    `json:"max_size_mb"`
	MaxBackups int    `json:"max_backups"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		HoNData: HoNData{
			TotalServers:              3,
			ServersPerCore:            2,
			StartingGamePort:          DefaultGamePort,
			StartingVoicePort:         DefaultVoicePort,
			ManagerPort:               DefaultManagerPort,
			APIPort:                   DefaultAPIPort,
			ProxyStartPort:            11297,
			VoiceProxyLocalStartPort:  11597,
			VoiceProxyRemoteStartPort: 11897,
			MasterServerURL:           "api.kongor.net",
			ChatAddress:               "96.127.149.202",
			ChatPort:                  11032,
			MaxIdleTime:               60,
			AllowBotMatches:           false,
			OverrideAffinity:          true,
		},
		ApplicationData: ApplicationData{
			Timers: TimerConfig{
				PatchCheckInterval:    120,
				VersionCheckInterval:  60,
				PublicIPCheckInterval: 1800,
				GeneralHealthInterval: 60,
				DiskCheckInterval:     3600,
				LagCheckInterval:      120,
				FilebeatCheckInterval: 10800,
				AutopingCheckInterval: 300,
				StatsPollingInterval:  10,
				HeartbeatInterval:     60,
				TaskCleanupInterval:   1800,
			},
			ReplayCleaner: ReplayCleanerConfig{
				Enabled:          true,
				CleanupTime:      "04:00",
				RetentionDays:    7,
				TmpRetentionDays: 1,
			},
			Discord: DiscordConfig{
				NotifyOnLag:   true,
				NotifyOnCrash: true,
				NotifyOnDisk:  true,
			},
			MQTT: MQTTConfig{
				Enabled:   true,
				BrokerURL: "mqtt.honfigurator.app",
				Port:      8883,
				UseTLS:    true,
			},
			Security: SecurityConfig{
				RateLimitRPS: 100,
				AuthDisabled: true,
			},
			Logging: LoggingConfig{
				Level:      "info",
				Directory:  "logs",
				MaxSizeMB:  10,
				MaxBackups: 5,
			},
		},
	}
}

// Load reads configuration from a JSON file.
func Load(configDir string) (*Config, error) {
	configPath := filepath.Join(configDir, DefaultConfigFile)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Str("path", configPath).Msg("config file not found, creating default")
			cfg := DefaultConfig()
			cfg.path = configPath
			if saveErr := cfg.Save(); saveErr != nil {
				return nil, fmt.Errorf("failed to save default config: %w", saveErr)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	cfg := DefaultConfig() // Start with defaults, then overlay
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	cfg.path = configPath
	log.Info().Str("path", configPath).Msg("configuration loaded")

	// Re-save config to persist any new default fields added in code updates.
	// This ensures config.json always reflects the complete set of options.
	if saveErr := cfg.Save(); saveErr != nil {
		log.Warn().Err(saveErr).Msg("failed to re-save config with updated defaults")
	}

	return cfg, nil
}

// Save writes the current configuration to disk.
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Ensure config directory exists
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(c.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	log.Debug().Str("path", c.path).Msg("configuration saved")
	return nil
}

// GetHoNData returns a copy of the HoN data configuration.
func (c *Config) GetHoNData() HoNData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.HoNData
}

// SetHoNData updates the HoN data configuration.
func (c *Config) SetHoNData(data HoNData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.HoNData = data
}

// GetApplicationData returns a copy of the application data configuration.
func (c *Config) GetApplicationData() ApplicationData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ApplicationData
}

// SetApplicationData updates the application data configuration.
func (c *Config) SetApplicationData(data ApplicationData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ApplicationData = data
}

// UpdateHoNField updates a specific field in HoN data.
func (c *Config) UpdateHoNField(key string, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Marshal current HoN data to map
	data, _ := json.Marshal(c.HoNData)
	m := make(map[string]interface{})
	json.Unmarshal(data, &m)

	// Update field
	m[key] = value

	// Unmarshal back
	updated, _ := json.Marshal(m)
	if err := json.Unmarshal(updated, &c.HoNData); err != nil {
		return fmt.Errorf("failed to update field %s: %w", key, err)
	}

	return nil
}

// UpdateAppField updates a specific field in application data.
func (c *Config) UpdateAppField(key string, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, _ := json.Marshal(c.ApplicationData)
	m := make(map[string]interface{})
	json.Unmarshal(data, &m)

	m[key] = value

	updated, _ := json.Marshal(m)
	if err := json.Unmarshal(updated, &c.ApplicationData); err != nil {
		return fmt.Errorf("failed to update field %s: %w", key, err)
	}

	return nil
}

// Path returns the config file path.
func (c *Config) Path() string {
	return c.path
}

// IsFirstRun returns true if the configuration needs initial setup.
func (c *Config) IsFirstRun() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.HoNData.Login == "" || c.HoNData.InstallDirectory == ""
}
