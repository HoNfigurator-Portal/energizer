// Package server implements the game server lifecycle management,
// including the central orchestrator (Manager), individual server
// instances, process control, and monitoring.
package server

import (
	"sync"
	"time"

	"github.com/energizer-project/energizer/internal/events"
)

// GameState encapsulates the current state of a game server instance.
// It is thread-safe and tracks both server status and game phase.
type GameState struct {
	mu sync.RWMutex

	// Current status and phase
	Status    events.GameStatus
	Phase     events.GamePhase

	// Match info
	MatchID     uint32
	MapName     string
	GameMode    string
	PlayerCount uint8

	// Player info
	Players     map[string]PlayerInfo

	// Timing
	StatusChangedAt time.Time
	PhaseChangedAt  time.Time
	StartedAt       time.Time

	// Telemetry
	Uptime      uint32
	CPUUsage    float32
	PlayerPings map[string]uint16

	// Lag tracking
	SkippedFrames    []SkippedFrame
	TotalLagEvents   int
	LastLagTime      time.Time
}

// PlayerInfo holds information about a connected player.
type PlayerInfo struct {
	Name      string    `json:"name"`
	ID        uint32    `json:"id"`
	PSR       float64   `json:"psr"`
	JoinedAt  time.Time `json:"joined_at"`
	Ping      uint16    `json:"ping"`
}

// SkippedFrame records a lag event (long frame).
type SkippedFrame struct {
	Timestamp time.Time `json:"timestamp"`
	Duration  uint32    `json:"duration_ms"`
}

// NewGameState creates a new GameState with initial values.
func NewGameState() *GameState {
	now := time.Now()
	return &GameState{
		Status:          events.GameStatusQueued,
		Phase:           events.GamePhaseIdle,
		Players:         make(map[string]PlayerInfo),
		PlayerPings:     make(map[string]uint16),
		SkippedFrames:   make([]SkippedFrame, 0),
		StatusChangedAt: now,
		PhaseChangedAt:  now,
	}
}

// SetStatus updates the server status and records the transition time.
func (s *GameState) SetStatus(status events.GameStatus) events.GameStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Status
	s.Status = status
	s.StatusChangedAt = time.Now()
	return old
}

// GetStatus returns the current server status.
func (s *GameState) GetStatus() events.GameStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// SetPhase updates the game phase.
func (s *GameState) SetPhase(phase events.GamePhase) events.GamePhase {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Phase
	s.Phase = phase
	s.PhaseChangedAt = time.Now()
	return old
}

// GetPhase returns the current game phase.
func (s *GameState) GetPhase() events.GamePhase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Phase
}

// UpdateTelemetry updates the server telemetry data from a status packet.
func (s *GameState) UpdateTelemetry(uptime uint32, cpuUsage float32, playerCount uint8,
	phase events.GamePhase, matchID uint32, pings map[string]uint16) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.Uptime = uptime
	s.CPUUsage = cpuUsage
	s.PlayerCount = playerCount
	s.MatchID = matchID
	s.PlayerPings = pings

	// Update phase if changed
	if s.Phase != phase {
		s.Phase = phase
		s.PhaseChangedAt = time.Now()
	}

	// Update status based on player count
	if playerCount > 0 && s.Status == events.GameStatusReady {
		s.Status = events.GameStatusOccupied
		s.StatusChangedAt = time.Now()
	} else if playerCount == 0 && s.Status == events.GameStatusOccupied {
		s.Status = events.GameStatusReady
		s.StatusChangedAt = time.Now()
	}
}

// AddPlayer adds a player to the state.
func (s *GameState) AddPlayer(name string, id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players[name] = PlayerInfo{
		Name:     name,
		ID:       id,
		JoinedAt: time.Now(),
	}
	s.PlayerCount = uint8(len(s.Players))
}

// RemovePlayer removes a player from the state.
func (s *GameState) RemovePlayer(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Players, name)
	s.PlayerCount = uint8(len(s.Players))
}

// GetPlayers returns a copy of the current player list.
func (s *GameState) GetPlayers() map[string]PlayerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]PlayerInfo, len(s.Players))
	for k, v := range s.Players {
		result[k] = v
	}
	return result
}

// AddLagEvent records a skipped frame / lag event.
func (s *GameState) AddLagEvent(duration uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SkippedFrames = append(s.SkippedFrames, SkippedFrame{
		Timestamp: time.Now(),
		Duration:  duration,
	})
	s.TotalLagEvents++
	s.LastLagTime = time.Now()
}

// GetLagEvents returns a copy of the skipped frame data.
func (s *GameState) GetLagEvents() []SkippedFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]SkippedFrame, len(s.SkippedFrames))
	copy(result, s.SkippedFrames)
	return result
}

// ClearLagEvents resets the skipped frame tracker.
func (s *GameState) ClearLagEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SkippedFrames = make([]SkippedFrame, 0)
}

// SetMatchInfo updates the current match information.
func (s *GameState) SetMatchInfo(matchID uint32, mapName, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MatchID = matchID
	s.MapName = mapName
	s.GameMode = mode
}

// Reset resets the game state to defaults (for server restart).
func (s *GameState) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.Status = events.GameStatusQueued
	s.Phase = events.GamePhaseIdle
	s.MatchID = 0
	s.MapName = ""
	s.GameMode = ""
	s.PlayerCount = 0
	s.Players = make(map[string]PlayerInfo)
	s.PlayerPings = make(map[string]uint16)
	s.SkippedFrames = make([]SkippedFrame, 0)
	s.Uptime = 0
	s.CPUUsage = 0
	s.StatusChangedAt = now
	s.PhaseChangedAt = now
}

// Snapshot returns a read-only snapshot of the current state.
func (s *GameState) Snapshot() GameStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	players := make(map[string]PlayerInfo, len(s.Players))
	for k, v := range s.Players {
		players[k] = v
	}

	return GameStateSnapshot{
		Status:          s.Status,
		Phase:           s.Phase,
		MatchID:         s.MatchID,
		MapName:         s.MapName,
		GameMode:        s.GameMode,
		PlayerCount:     s.PlayerCount,
		Players:         players,
		Uptime:          s.Uptime,
		CPUUsage:        s.CPUUsage,
		TotalLagEvents:  s.TotalLagEvents,
		StatusChangedAt: s.StatusChangedAt,
		PhaseChangedAt:  s.PhaseChangedAt,
	}
}

// GameStateSnapshot is an immutable snapshot of a game state.
type GameStateSnapshot struct {
	Status          events.GameStatus       `json:"status"`
	Phase           events.GamePhase        `json:"phase"`
	MatchID         uint32                  `json:"match_id"`
	MapName         string                  `json:"map_name"`
	GameMode        string                  `json:"game_mode"`
	PlayerCount     uint8                   `json:"player_count"`
	Players         map[string]PlayerInfo   `json:"players"`
	Uptime          uint32                  `json:"uptime"`
	CPUUsage        float32                 `json:"cpu_usage"`
	TotalLagEvents  int                     `json:"total_lag_events"`
	StatusChangedAt time.Time               `json:"status_changed_at"`
	PhaseChangedAt  time.Time               `json:"phase_changed_at"`
}
