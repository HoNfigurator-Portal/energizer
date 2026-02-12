// Package util provides utility functions used throughout the Energizer application.
package util

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// LogConfig holds configuration for the logging system.
type LogConfig struct {
	Level      string `json:"level"`
	Directory  string `json:"directory"`
	MaxSizeMB  int    `json:"max_size_mb"`
	MaxBackups int    `json:"max_backups"`
	Console    bool   `json:"console"`
}

// DefaultLogConfig returns the default logging configuration.
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Level:      "info",
		Directory:  "logs",
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    true,
	}
}

// InitLogger initializes the zerolog global logger with file and console output.
func InitLogger(cfg LogConfig) error {
	// Parse log level
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Configure time format
	zerolog.TimeFieldFormat = time.RFC3339

	// Create log directory
	if err := os.MkdirAll(cfg.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create log directory %s: %w", cfg.Directory, err)
	}

	// Create log file with date-based name
	logFileName := fmt.Sprintf("energizer_%s.log", time.Now().Format("2006-01-02"))
	logFilePath := filepath.Join(cfg.Directory, logFileName)

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logFilePath, err)
	}

	// Build writers
	var writers []io.Writer

	// File writer (JSON format for machine parsing)
	writers = append(writers, logFile)

	// Console writer (human-readable format)
	if cfg.Console {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05",
			NoColor:    false,
		}
		writers = append(writers, consoleWriter)
	}

	// Multi-writer: both file and console
	multi := zerolog.MultiLevelWriter(writers...)

	// Set global logger
	log.Logger = zerolog.New(multi).
		With().
		Timestamp().
		Str("app", "energizer").
		Caller().
		Logger()

	log.Info().
		Str("level", level.String()).
		Str("log_file", logFilePath).
		Msg("logger initialized")

	// Clean up old log files
	go cleanOldLogs(cfg.Directory, cfg.MaxBackups)

	return nil
}

// cleanOldLogs removes log files older than the retention limit.
func cleanOldLogs(directory string, maxBackups int) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}

	var logFiles []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".log" {
			logFiles = append(logFiles, entry)
		}
	}

	// Remove oldest files if exceeding max backups
	if len(logFiles) > maxBackups {
		// Sort by modification time (oldest first) and remove excess
		for i := 0; i < len(logFiles)-maxBackups; i++ {
			path := filepath.Join(directory, logFiles[i].Name())
			os.Remove(path)
			log.Debug().Str("file", path).Msg("removed old log file")
		}
	}
}

// ComponentLogger creates a logger with a component name field.
func ComponentLogger(component string) zerolog.Logger {
	return log.With().Str("component", component).Logger()
}
