// Package scheduler implements background task scheduling for Energizer,
// including replay file cleanup and daily statistics collection.
package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
)

// Scheduler manages periodic background tasks.
type Scheduler struct {
	cfg      *config.Config
	eventBus *events.EventBus
}

// NewScheduler creates a new task scheduler.
func NewScheduler(cfg *config.Config, eventBus *events.EventBus) *Scheduler {
	return &Scheduler{
		cfg:      cfg,
		eventBus: eventBus,
	}
}

// Start begins running all scheduled tasks.
func (s *Scheduler) Start(ctx context.Context) {
	log.Info().Msg("scheduler started")

	// Replay cleaner - runs at configured time daily
	if s.cfg.ApplicationData.ReplayCleaner.Enabled {
		go s.runReplayCleanerLoop(ctx)
	}

	// Stats collection - runs daily
	go s.runStatsCollectionLoop(ctx)

	// Block until context is cancelled
	<-ctx.Done()
	log.Info().Msg("scheduler stopped")
}

// runReplayCleanerLoop runs the replay cleaner at the configured time.
func (s *Scheduler) runReplayCleanerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Calculate time until next cleanup
		nextRun := s.calculateNextCleanupTime()
		sleepDuration := time.Until(nextRun)

		if sleepDuration <= 0 {
			sleepDuration = 24 * time.Hour
		}

		log.Info().
			Time("next_run", nextRun).
			Dur("sleep", sleepDuration).
			Msg("replay cleaner scheduled")

		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDuration):
			s.runReplayCleaner()
		}
	}
}

// runReplayCleaner performs the actual cleanup of old replay files.
func (s *Scheduler) runReplayCleaner() {
	honData := s.cfg.GetHoNData()
	cleanerCfg := s.cfg.ApplicationData.ReplayCleaner

	replayDir := filepath.Join(honData.HomeDirectory, "replays")
	retentionDays := cleanerCfg.RetentionDays
	tmpRetentionDays := cleanerCfg.TmpRetentionDays

	log.Info().
		Str("directory", replayDir).
		Int("retention_days", retentionDays).
		Msg("running replay cleaner")

	var (
		deletedCount int
		deletedSize  int64
	)

	// Walk the replay directory
	err := filepath.Walk(replayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if info.IsDir() {
			return nil
		}

		age := time.Since(info.ModTime())
		ext := strings.ToLower(filepath.Ext(info.Name()))

		var shouldDelete bool

		switch ext {
		case ".honreplay":
			shouldDelete = age > time.Duration(retentionDays)*24*time.Hour
		case ".tmp", ".clog":
			shouldDelete = age > time.Duration(tmpRetentionDays)*24*time.Hour
		}

		if shouldDelete {
			if err := os.Remove(path); err == nil {
				deletedCount++
				deletedSize += info.Size()
				log.Debug().Str("file", info.Name()).Msg("deleted old file")
			}
		}

		return nil
	})

	if err != nil {
		log.Warn().Err(err).Msg("replay cleaner encountered errors")
	}

	log.Info().
		Int("deleted_files", deletedCount).
		Str("freed_space", formatBytes(deletedSize)).
		Msg("replay cleaner completed")
}

// runStatsCollectionLoop collects daily statistics.
func (s *Scheduler) runStatsCollectionLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.collectStats()
		}
	}
}

// collectStats gathers and stores daily statistics.
func (s *Scheduler) collectStats() {
	honData := s.cfg.GetHoNData()
	replayDir := filepath.Join(honData.HomeDirectory, "replays")

	// Count replay files
	replayCount := 0
	entries, err := os.ReadDir(replayDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".honreplay") {
				replayCount++
			}
		}
	}

	log.Info().
		Int("replay_count", replayCount).
		Msg("daily stats collected")
}

// calculateNextCleanupTime returns the next time the cleanup should run.
func (s *Scheduler) calculateNextCleanupTime() time.Time {
	cleanupTime := s.cfg.ApplicationData.ReplayCleaner.CleanupTime
	parts := strings.Split(cleanupTime, ":")

	hour, minute := 4, 0 // Default: 4:00 AM
	if len(parts) >= 2 {
		fmt.Sscanf(parts[0], "%d", &hour)
		fmt.Sscanf(parts[1], "%d", &minute)
	}

	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}

// formatBytes formats bytes into human-readable format.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
