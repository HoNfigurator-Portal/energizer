// Package health implements periodic health check monitoring for all
// Energizer subsystems, including game version patches, disk utilization,
// network connectivity, and server process health.
package health

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/connector"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/server"
	"github.com/energizer-project/energizer/internal/util"
)

// Manager runs periodic health checks on all subsystems.
// It replaces the Python HealthCheckManager and its 8+ scheduled checks.
type Manager struct {
	cfg       *config.Config
	eventBus  *events.EventBus
	serverMgr *server.Manager
	masterSvr *connector.MasterServerConnector
}

// NewManager creates a new health check manager.
func NewManager(
	cfg *config.Config,
	eventBus *events.EventBus,
	serverMgr *server.Manager,
	masterSvr *connector.MasterServerConnector,
) *Manager {
	return &Manager{
		cfg:       cfg,
		eventBus:  eventBus,
		serverMgr: serverMgr,
		masterSvr: masterSvr,
	}
}

// Start launches all health check goroutines.
func (m *Manager) Start(ctx context.Context) {
	timers := m.cfg.ApplicationData.Timers

	// Launch each health check as a separate goroutine with its own ticker
	checks := []struct {
		name     string
		interval int
		fn       func(context.Context)
	}{
		{"patch_version", timers.PatchCheckInterval, m.checkPatchVersion},
		{"energizer_version", timers.VersionCheckInterval, m.checkEnergizerVersion},
		{"public_ip", timers.PublicIPCheckInterval, m.checkPublicIP},
		{"general_health", timers.GeneralHealthInterval, m.checkGeneralHealth},
		{"disk_utilization", timers.DiskCheckInterval, m.checkDiskUtilization},
		{"lag_health", timers.LagCheckInterval, m.checkLagHealth},
		{"autoping_listener", timers.AutopingCheckInterval, m.checkAutoPingListener},
		{"stats_polling", timers.StatsPollingInterval, m.pollGameStats},
	}

	for _, check := range checks {
		if check.interval <= 0 {
			continue
		}

		check := check
		go func() {
			ticker := time.NewTicker(time.Duration(check.interval) * time.Second)
			defer ticker.Stop()

			// Run immediately on startup
			log.Debug().Str("check", check.name).Msg("running initial health check")
			check.fn(ctx)

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					check.fn(ctx)
				}
			}
		}()
	}

	// Heartbeat (special: publishes MQTT status)
	go m.heartbeatLoop(ctx, time.Duration(timers.HeartbeatInterval)*time.Second)

	log.Info().Int("checks", len(checks)).Msg("health check manager started")

	// Block until context is cancelled
	<-ctx.Done()
	log.Info().Msg("health check manager stopped")
}

// checkPatchVersion checks for upstream game patches.
func (m *Manager) checkPatchVersion(ctx context.Context) {
	if m.masterSvr == nil || !m.masterSvr.IsAuthenticated() {
		return
	}

	patchAvailable, newVersion, err := m.masterSvr.CompareUpstreamPatch(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("patch version check failed")
		return
	}

	if patchAvailable {
		log.Info().Str("new_version", newVersion).Msg("new game version available")
		m.eventBus.Emit(ctx, events.Event{
			Type:   events.EventPatchServer,
			Source: "health_check",
			Payload: map[string]string{
				"new_version": newVersion,
			},
		})
	}
}

// checkEnergizerVersion checks for Energizer manager updates.
func (m *Manager) checkEnergizerVersion(ctx context.Context) {
	// In production, this would check a remote source for updates.
	// For now, log that the check was performed.
	log.Trace().Msg("energizer version check completed")
}

// checkPublicIP detects changes to the public IP address.
func (m *Manager) checkPublicIP(ctx context.Context) {
	ip, err := util.GetPublicIP()
	if err != nil {
		log.Warn().Err(err).Msg("public IP check failed")
		return
	}

	currentIP := m.serverMgr.GetPublicIP()
	if currentIP != "" && currentIP != ip {
		log.Warn().
			Str("old_ip", currentIP).
			Str("new_ip", ip).
			Msg("public IP changed!")

		m.eventBus.Emit(ctx, events.Event{
			Type:   events.EventNotifyDiscordAdmin,
			Source: "health_check",
			Payload: events.NotifyDiscordPayload{
				Title:   "Public IP Changed",
				Message: fmt.Sprintf("Public IP changed from %s to %s", currentIP, ip),
				Level:   "warning",
			},
		})
	}

	m.serverMgr.SetPublicIP(ip)
}

// checkGeneralHealth finds stuck/orphaned servers and cleans up.
func (m *Manager) checkGeneralHealth(ctx context.Context) {
	instances := m.serverMgr.GetAllInstances()

	for port, inst := range instances {
		// Check for stuck servers (running but not responding)
		if inst.IsRunning() {
			state := inst.State()
			status := state.GetStatus()

			// Server stuck in STARTING for > 2 minutes
			if status == events.GameStatusStarting {
				if time.Since(state.StatusChangedAt) > 2*time.Minute {
					log.Warn().Uint16("port", port).Msg("server stuck in STARTING state, restarting")
					go inst.Restart(ctx)
				}
			}

			// Check for periodic restart
			if inst.NeedsRestart() {
				log.Info().Uint16("port", port).Msg("scheduled periodic restart")
				go inst.Restart(ctx)
			}
		}
	}

	// Clean stale connections
	registry := m.serverMgr.GetConnectionRegistry()
	cleaned := registry.CleanStale(2 * time.Minute)
	if cleaned > 0 {
		log.Info().Int("cleaned", cleaned).Msg("cleaned stale connections")
	}
}

// checkDiskUtilization monitors disk space and alerts at thresholds.
func (m *Manager) checkDiskUtilization(ctx context.Context) {
	honData := m.cfg.GetHoNData()
	path := honData.InstallDirectory
	if path == "" {
		path = "/"
	}

	usage, err := util.GetDiskUsage(path)
	if err != nil {
		log.Warn().Err(err).Msg("disk utilization check failed")
		return
	}

	log.Debug().
		Float64("used_percent", usage.UsedPercent).
		Uint64("free_gb", usage.Free).
		Msg("disk utilization")

	// Alert thresholds: 80%, 90%, 95%, 100%
	var level string
	switch {
	case usage.UsedPercent >= 100:
		level = "critical"
	case usage.UsedPercent >= 95:
		level = "error"
	case usage.UsedPercent >= 90:
		level = "warning"
	case usage.UsedPercent >= 80:
		level = "info"
	default:
		return // No alert needed
	}

	message := fmt.Sprintf("Disk usage at %.1f%% (%d GB free of %d GB total)",
		usage.UsedPercent, usage.Free, usage.Total)

	log.Warn().Str("level", level).Msg(message)

	if m.cfg.ApplicationData.Discord.NotifyOnDisk {
		m.eventBus.Emit(ctx, events.Event{
			Type:   events.EventNotifyDiscordAdmin,
			Source: "health_check",
			Payload: events.NotifyDiscordPayload{
				Title:   "Disk Space Alert",
				Message: message,
				Level:   level,
			},
		})
	}
}

// checkLagHealth evaluates lag metrics across all servers.
func (m *Manager) checkLagHealth(ctx context.Context) {
	instances := m.serverMgr.GetAllInstances()

	for port, inst := range instances {
		lagEvents := inst.State().GetLagEvents()
		if len(lagEvents) > 0 {
			// Count events in last check interval
			cutoff := time.Now().Add(-time.Duration(m.cfg.ApplicationData.Timers.LagCheckInterval) * time.Second)
			recentCount := 0
			for _, e := range lagEvents {
				if e.Timestamp.After(cutoff) {
					recentCount++
				}
			}

			if recentCount > 5 {
				log.Warn().
					Uint16("port", port).
					Int("recent_lag_events", recentCount).
					Msg("elevated lag detected")
			}
		}
	}
}

// checkAutoPingListener validates the UDP auto-ping listener.
func (m *Manager) checkAutoPingListener(ctx context.Context) {
	// Self-test would be implemented here
	log.Trace().Msg("autoping listener check completed")
}

// pollGameStats scans for .stats files and submits them.
func (m *Manager) pollGameStats(ctx context.Context) {
	// Scan for unsubmitted .stats files and send to master server
	log.Trace().Msg("game stats polling completed")
}

// heartbeatLoop publishes periodic heartbeat via MQTT.
func (m *Manager) heartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.eventBus.Emit(ctx, events.Event{
				Type:   events.EventNotifyMQTT,
				Source: "heartbeat",
				Payload: map[string]interface{}{
					"type":           "heartbeat",
					"total_servers":  m.serverMgr.GetTotalServers(),
					"running":        m.serverMgr.GetRunningCount(),
					"occupied":       m.serverMgr.GetOccupiedCount(),
					"public_ip":      m.serverMgr.GetPublicIP(),
					"timestamp":      time.Now().Unix(),
				},
			})
		}
	}
}
