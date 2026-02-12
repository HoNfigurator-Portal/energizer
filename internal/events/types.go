// Package events defines event types and enumerations for the Energizer event system.
package events

// EventType represents the type of event emitted through the EventBus.
type EventType string

const (
	// Server lifecycle events
	EventStartGameServers    EventType = "start_game_servers"
	EventAddGameServers      EventType = "add_game_servers"
	EventRemoveGameServers   EventType = "remove_game_servers"
	EventShutdownServer      EventType = "cmd_shutdown_server"
	EventWakeServer          EventType = "cmd_wake_server"
	EventSleepServer         EventType = "cmd_sleep_server"
	EventMessageServer       EventType = "cmd_message_server"
	EventCommandServer       EventType = "cmd_command_server"

	// Connection events
	EventServerAnnounce      EventType = "server_announce"
	EventServerClosed        EventType = "server_closed"
	EventServerStatus        EventType = "server_status"
	EventLongFrame           EventType = "long_frame"
	EventLobbyCreated        EventType = "lobby_created"
	EventLobbyClosed         EventType = "lobby_closed"
	EventPlayerConnection    EventType = "player_connection"
	EventCowMasterResponse   EventType = "cowmaster_response"
	EventReplayStatus        EventType = "replay_status"

	// Upstream events
	EventAuthenticateChat    EventType = "authenticate_to_chat_svr"
	EventHandleReplayRequest EventType = "handle_replay_request"
	EventPatchServer         EventType = "patch_server"
	EventUpdate              EventType = "update"

	// Notification events
	EventNotifyDiscordAdmin  EventType = "notify_discord_admin"
	EventNotifyMQTT          EventType = "notify_mqtt"

	// System events
	EventForkFromCowMaster   EventType = "fork_server_from_cowmaster"
	EventConfigChanged       EventType = "config_changed"
	EventShutdown            EventType = "shutdown"
)

// GameStatus represents the current status of a game server instance.
type GameStatus int

const (
	GameStatusUnknown  GameStatus = iota
	GameStatusQueued
	GameStatusStarting
	GameStatusReady
	GameStatusOccupied
	GameStatusSleeping
	GameStatusStopped
)

// gameStatusStrings maps GameStatus values to their lowercase JSON string representation.
var gameStatusStrings = map[GameStatus]string{
	GameStatusUnknown:  "unknown",
	GameStatusQueued:   "queued",
	GameStatusStarting: "starting",
	GameStatusReady:    "ready",
	GameStatusOccupied: "occupied",
	GameStatusSleeping: "sleeping",
	GameStatusStopped:  "stopped",
}

// String returns the string representation of GameStatus.
func (s GameStatus) String() string {
	if str, ok := gameStatusStrings[s]; ok {
		return str
	}
	return "unknown"
}

// MarshalJSON serializes GameStatus as a JSON string (e.g. "ready").
func (s GameStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// GamePhase represents the current phase of a game match.
type GamePhase int

const (
	GamePhaseIdle        GamePhase = iota
	GamePhaseInLobby
	GamePhaseBanning
	GamePhasePicking
	GamePhaseLoading
	GamePhasePreparation
	GamePhaseMatchStarted
	GamePhaseGameEnding
	GamePhaseGameEnded
)

// gamePhaseStrings maps GamePhase values to their lowercase JSON string representation.
var gamePhaseStrings = map[GamePhase]string{
	GamePhaseIdle:         "idle",
	GamePhaseInLobby:      "in_lobby",
	GamePhaseBanning:      "banning",
	GamePhasePicking:      "picking",
	GamePhaseLoading:      "loading",
	GamePhasePreparation:  "preparation",
	GamePhaseMatchStarted: "match_started",
	GamePhaseGameEnding:   "game_ending",
	GamePhaseGameEnded:    "game_ended",
}

// String returns the string representation of GamePhase.
func (p GamePhase) String() string {
	if str, ok := gamePhaseStrings[p]; ok {
		return str
	}
	return "idle"
}

// MarshalJSON serializes GamePhase as a JSON string (e.g. "idle").
func (p GamePhase) MarshalJSON() ([]byte, error) {
	return []byte(`"` + p.String() + `"`), nil
}

// ReplayStatus represents the status of a replay upload/request.
type ReplayStatus int

const (
	ReplayStatusNone       ReplayStatus = iota
	ReplayStatusRequested
	ReplayStatusQueued
	ReplayStatusUploading
	ReplayStatusUploaded
	ReplayStatusFailed
	ReplayStatusNotFound
	ReplayStatusReady
)

// String returns the string representation of ReplayStatus.
func (r ReplayStatus) String() string {
	switch r {
	case ReplayStatusNone:
		return "NONE"
	case ReplayStatusRequested:
		return "REQUESTED"
	case ReplayStatusQueued:
		return "QUEUED"
	case ReplayStatusUploading:
		return "UPLOADING"
	case ReplayStatusUploaded:
		return "UPLOADED"
	case ReplayStatusFailed:
		return "FAILED"
	case ReplayStatusNotFound:
		return "NOT_FOUND"
	case ReplayStatusReady:
		return "READY"
	default:
		return "NONE"
	}
}

// GameServerCommand represents binary command bytes for game server communication.
type GameServerCommand byte

const (
	CmdServerAnnounce    GameServerCommand = 0x40
	CmdServerClosed      GameServerCommand = 0x41
	CmdServerStatus      GameServerCommand = 0x42
	CmdLongFrame         GameServerCommand = 0x43
	CmdLobbyCreated      GameServerCommand = 0x44
	CmdLobbyClosed       GameServerCommand = 0x45
	CmdPlayerConnection  GameServerCommand = 0x47
	CmdCowMasterResponse GameServerCommand = 0x49
	CmdReplayStatus      GameServerCommand = 0x4A
)

// Event represents a single event in the system.
type Event struct {
	Type    EventType
	Source  string
	Payload interface{}
}

// ServerAnnouncePayload contains data from a server announce packet (0x40).
type ServerAnnouncePayload struct {
	Port uint16
}

// ServerStatusPayload contains telemetry data from a server status packet (0x42).
type ServerStatusPayload struct {
	Port        uint16
	Uptime      uint32
	CPUUsage    float32
	PlayerCount uint8
	GamePhase   GamePhase
	MatchID     uint32
	PlayerPings map[string]uint16
}

// LobbyCreatedPayload contains data from a lobby created packet (0x44).
type LobbyCreatedPayload struct {
	Port    uint16
	MatchID uint32
	MapName string
	Mode    string
}

// PlayerConnectionPayload contains data from a player connection event (0x47).
type PlayerConnectionPayload struct {
	Port       uint16
	PlayerName string
	PlayerID   uint32
	Connected  bool
}

// LongFramePayload contains data from a long frame packet (0x43).
type LongFramePayload struct {
	Port          uint16
	FrameDuration uint32
}

// ReplayStatusPayload contains data from a replay status update (0x4A).
type ReplayStatusPayload struct {
	Port    uint16
	MatchID uint32
	Status  ReplayStatus
}

// CowMasterResponsePayload contains data from a CowMaster fork response (0x49).
type CowMasterResponsePayload struct {
	Port    uint16
	Success bool
	PID     int
}

// ServerCommandPayload is used for sending commands to game servers.
type ServerCommandPayload struct {
	Port    uint16
	Command string
	Args    []string
}

// NotifyDiscordPayload is used for sending Discord notifications.
type NotifyDiscordPayload struct {
	Title   string
	Message string
	Level   string // "info", "warning", "error"
}

// ConfigChangedPayload is emitted when configuration changes occur.
type ConfigChangedPayload struct {
	Section string
	Key     string
	Value   interface{}
}
