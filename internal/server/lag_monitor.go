package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/events"
)

// LagMonitor tracks and analyzes lag events across all game servers.
// It aggregates skipped frame data, triggers alerts, and provides
// the data needed for the /get_skipped_frame_data API endpoint.
type LagMonitor struct {
	mu       sync.RWMutex
	eventBus *events.EventBus

	// Per-port lag data
	portData map[uint16]*PortLagData

	// Thresholds
	warningThreshold  int
	criticalThreshold int
}

// PortLagData holds lag tracking data for a single server port.
type PortLagData struct {
	Port           uint16         `json:"port"`
	TotalEvents    int            `json:"total_events"`
	EventsThisHour int            `json:"events_this_hour"`
	LastEventTime  time.Time      `json:"last_event_time"`
	MaxDuration    uint32         `json:"max_duration_ms"`
	AvgDuration    float64        `json:"avg_duration_ms"`
	History        []LagEvent     `json:"history"`
	HourlyBuckets  map[int]int    `json:"hourly_buckets"`
}

// LagEvent represents a single lag event.
type LagEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Duration  uint32    `json:"duration_ms"`
	MatchID   uint32    `json:"match_id,omitempty"`
}

// NewLagMonitor creates a new lag monitor.
func NewLagMonitor(eventBus *events.EventBus) *LagMonitor {
	lm := &LagMonitor{
		eventBus:          eventBus,
		portData:          make(map[uint16]*PortLagData),
		warningThreshold:  LagWarningThreshold,
		criticalThreshold: LagCriticalThreshold,
	}

	// Subscribe to lag events
	eventBus.Subscribe(events.EventLongFrame, "lag_monitor", lm.handleLagEvent)

	return lm
}

// handleLagEvent processes incoming lag events.
func (lm *LagMonitor) handleLagEvent(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.LongFramePayload)
	if !ok {
		return nil
	}

	lm.mu.Lock()
	defer lm.mu.Unlock()

	data, ok := lm.portData[payload.Port]
	if !ok {
		data = &PortLagData{
			Port:          payload.Port,
			History:       make([]LagEvent, 0, 100),
			HourlyBuckets: make(map[int]int),
		}
		lm.portData[payload.Port] = data
	}

	now := time.Now()
	lagEvent := LagEvent{
		Timestamp: now,
		Duration:  payload.FrameDuration,
	}

	data.TotalEvents++
	data.LastEventTime = now
	data.History = append(data.History, lagEvent)

	// Update max duration
	if payload.FrameDuration > data.MaxDuration {
		data.MaxDuration = payload.FrameDuration
	}

	// Update average duration
	totalDuration := uint64(0)
	for _, e := range data.History {
		totalDuration += uint64(e.Duration)
	}
	data.AvgDuration = float64(totalDuration) / float64(len(data.History))

	// Update hourly bucket
	hour := now.Hour()
	data.HourlyBuckets[hour]++

	// Count events in the last hour
	oneHourAgo := now.Add(-1 * time.Hour)
	eventsThisHour := 0
	for _, e := range data.History {
		if e.Timestamp.After(oneHourAgo) {
			eventsThisHour++
		}
	}
	data.EventsThisHour = eventsThisHour

	// Trim history to last 1000 events
	if len(data.History) > 1000 {
		data.History = data.History[len(data.History)-1000:]
	}

	return nil
}

// GetPortData returns lag data for a specific port.
func (lm *LagMonitor) GetPortData(port uint16) (*PortLagData, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	data, ok := lm.portData[port]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *data
	return &copy, true
}

// GetAllPortData returns lag data for all ports.
func (lm *LagMonitor) GetAllPortData() map[uint16]*PortLagData {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	result := make(map[uint16]*PortLagData, len(lm.portData))
	for k, v := range lm.portData {
		copy := *v
		result[k] = &copy
	}
	return result
}

// CheckThresholds evaluates all ports against lag thresholds.
func (lm *LagMonitor) CheckThresholds() []LagAlert {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var alerts []LagAlert
	for port, data := range lm.portData {
		if data.EventsThisHour >= lm.criticalThreshold {
			alerts = append(alerts, LagAlert{
				Port:     port,
				Level:    "critical",
				Events:   data.EventsThisHour,
				Message:  fmt.Sprintf("Server on port %d: %d lag events in the last hour", port, data.EventsThisHour),
			})
		} else if data.EventsThisHour >= lm.warningThreshold {
			alerts = append(alerts, LagAlert{
				Port:     port,
				Level:    "warning",
				Events:   data.EventsThisHour,
				Message:  fmt.Sprintf("Server on port %d: %d lag events in the last hour", port, data.EventsThisHour),
			})
		}
	}
	return alerts
}

// LagAlert represents a lag threshold alert.
type LagAlert struct {
	Port    uint16 `json:"port"`
	Level   string `json:"level"`
	Events  int    `json:"events"`
	Message string `json:"message"`
}

// Start begins periodic lag threshold checks.
func (lm *LagMonitor) Start(ctx context.Context, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			alerts := lm.CheckThresholds()
			for _, alert := range alerts {
				log.Warn().
					Uint16("port", alert.Port).
					Str("level", alert.Level).
					Int("events", alert.Events).
					Msg("lag threshold alert")

				if alert.Level == "critical" {
					lm.eventBus.Emit(ctx, events.Event{
						Type:   events.EventNotifyDiscordAdmin,
						Source: fmt.Sprintf("lag_monitor:%d", alert.Port),
						Payload: events.NotifyDiscordPayload{
							Title:   "Lag Alert - Critical",
							Message: alert.Message,
							Level:   "error",
						},
					})
				}
			}
		}
	}
}
