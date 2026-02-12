package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/events"
)

// GameManagerParser parses binary packets from the Game Server <-> Manager protocol.
// This replaces the Python GameManagerParser class from packet_parser.py.
type GameManagerParser struct {
	logger zerolog.Logger
}

// NewGameManagerParser creates a new parser for game-manager protocol.
func NewGameManagerParser() *GameManagerParser {
	return &GameManagerParser{
		logger: log.With().Str("component", "gm_parser").Logger(),
	}
}

// ReadPacket reads a single length-prefixed packet from a reader.
// Packet format: [2-byte LE length][payload bytes...]
// Returns the raw packet bytes (excluding length prefix).
func ReadPacket(r io.Reader) ([]byte, error) {
	// Read 2-byte length prefix
	var length uint16
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read packet length: %w", err)
	}

	if length == 0 {
		return nil, fmt.Errorf("received zero-length packet")
	}

	if length > MaxPacketSize {
		return nil, fmt.Errorf("packet too large: %d bytes (max %d)", length, MaxPacketSize)
	}

	// Read payload
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("failed to read packet payload (%d bytes): %w", length, err)
	}

	return payload, nil
}

// WritePacket writes a length-prefixed packet to a writer.
func WritePacket(w io.Writer, data []byte) error {
	length := uint16(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return fmt.Errorf("failed to write packet length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write packet data: %w", err)
	}
	return nil
}

// Parse processes a raw packet and returns a structured event.
func (p *GameManagerParser) Parse(data []byte) (*events.Event, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("empty packet")
	}

	cmd := data[0]
	payload := data[1:]
	reader := bytes.NewReader(payload)

	switch cmd {
	case PktServerAnnounce:
		return p.parseServerAnnounce(reader)
	case PktServerClosed:
		return p.parseServerClosed(reader)
	case PktServerStatus:
		return p.parseServerStatus(reader)
	case PktLongFrame:
		return p.parseLongFrame(reader)
	case PktLobbyCreated:
		return p.parseLobbyCreated(reader)
	case PktLobbyClosed:
		return p.parseLobbyClosed(reader)
	case PktPlayerConnection:
		return p.parsePlayerConnection(reader)
	case PktCowMasterResponse:
		return p.parseCowMasterResponse(reader)
	case PktReplayStatus:
		return p.parseReplayStatus(reader)
	default:
		p.logger.Warn().
			Uint8("command", cmd).
			Int("payload_len", len(payload)).
			Msg("unknown packet command")
		return nil, fmt.Errorf("unknown command: 0x%02X", cmd)
	}
}

// parseServerAnnounce handles packet 0x40: server hello with port.
func (p *GameManagerParser) parseServerAnnounce(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse server announce: %w", err)
	}

	p.logger.Debug().Uint16("port", port).Msg("server announce")

	return &events.Event{
		Type:   events.EventServerAnnounce,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.ServerAnnouncePayload{
			Port: port,
		},
	}, nil
}

// parseServerClosed handles packet 0x41: server shutting down.
func (p *GameManagerParser) parseServerClosed(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse server closed: %w", err)
	}

	p.logger.Info().Uint16("port", port).Msg("server closed")

	return &events.Event{
		Type:   events.EventServerClosed,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.ServerAnnouncePayload{
			Port: port,
		},
	}, nil
}

// parseServerStatus handles packet 0x42: server telemetry.
// Format: [port:2][uptime:4][cpu:4][player_count:1][game_phase:1][match_id:4]
//
//	[per-player pings: player_count * (name_len:1, name:var, ping:2)]
func (p *GameManagerParser) parseServerStatus(r *bytes.Reader) (*events.Event, error) {
	var (
		port        uint16
		uptime      uint32
		cpuUsage    float32
		playerCount uint8
		gamePhase   uint8
		matchID     uint32
	)

	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse status port: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &uptime); err != nil {
		return nil, fmt.Errorf("failed to parse status uptime: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &cpuUsage); err != nil {
		return nil, fmt.Errorf("failed to parse status cpu: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &playerCount); err != nil {
		return nil, fmt.Errorf("failed to parse status player count: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &gamePhase); err != nil {
		return nil, fmt.Errorf("failed to parse status game phase: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &matchID); err != nil {
		return nil, fmt.Errorf("failed to parse status match id: %w", err)
	}

	// Parse per-player ping data
	playerPings := make(map[string]uint16)
	for i := uint8(0); i < playerCount; i++ {
		name, err := readString(r)
		if err != nil {
			break // Remaining data may be truncated
		}

		var ping uint16
		if err := binary.Read(r, binary.LittleEndian, &ping); err != nil {
			break
		}
		playerPings[name] = ping
	}

	p.logger.Trace().
		Uint16("port", port).
		Uint32("uptime", uptime).
		Uint8("players", playerCount).
		Uint8("phase", gamePhase).
		Msg("server status")

	return &events.Event{
		Type:   events.EventServerStatus,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.ServerStatusPayload{
			Port:        port,
			Uptime:      uptime,
			CPUUsage:    cpuUsage,
			PlayerCount: playerCount,
			GamePhase:   events.GamePhase(gamePhase),
			MatchID:     matchID,
			PlayerPings: playerPings,
		},
	}, nil
}

// parseLongFrame handles packet 0x43: lag detection.
func (p *GameManagerParser) parseLongFrame(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	var duration uint32

	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse long frame port: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &duration); err != nil {
		return nil, fmt.Errorf("failed to parse long frame duration: %w", err)
	}

	p.logger.Warn().
		Uint16("port", port).
		Uint32("duration_ms", duration).
		Msg("long frame detected")

	return &events.Event{
		Type:   events.EventLongFrame,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.LongFramePayload{
			Port:          port,
			FrameDuration: duration,
		},
	}, nil
}

// parseLobbyCreated handles packet 0x44: match lobby created.
func (p *GameManagerParser) parseLobbyCreated(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	var matchID uint32

	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse lobby port: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &matchID); err != nil {
		return nil, fmt.Errorf("failed to parse lobby match id: %w", err)
	}

	mapName, _ := readString(r)
	mode, _ := readString(r)

	p.logger.Info().
		Uint16("port", port).
		Uint32("match_id", matchID).
		Str("map", mapName).
		Str("mode", mode).
		Msg("lobby created")

	return &events.Event{
		Type:   events.EventLobbyCreated,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.LobbyCreatedPayload{
			Port:    port,
			MatchID: matchID,
			MapName: mapName,
			Mode:    mode,
		},
	}, nil
}

// parseLobbyClosed handles packet 0x45: lobby closed.
func (p *GameManagerParser) parseLobbyClosed(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse lobby closed port: %w", err)
	}

	p.logger.Info().Uint16("port", port).Msg("lobby closed")

	return &events.Event{
		Type:   events.EventLobbyClosed,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.ServerAnnouncePayload{Port: port},
	}, nil
}

// parsePlayerConnection handles packet 0x47: player connect/disconnect.
func (p *GameManagerParser) parsePlayerConnection(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	var playerID uint32
	var connected uint8

	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse player connection port: %w", err)
	}

	playerName, err := readString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse player name: %w", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &playerID); err != nil {
		return nil, fmt.Errorf("failed to parse player id: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &connected); err != nil {
		return nil, fmt.Errorf("failed to parse connected flag: %w", err)
	}

	p.logger.Info().
		Uint16("port", port).
		Str("player", playerName).
		Uint32("player_id", playerID).
		Bool("connected", connected == 1).
		Msg("player connection event")

	return &events.Event{
		Type:   events.EventPlayerConnection,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.PlayerConnectionPayload{
			Port:       port,
			PlayerName: playerName,
			PlayerID:   playerID,
			Connected:  connected == 1,
		},
	}, nil
}

// parseCowMasterResponse handles packet 0x49: fork response.
func (p *GameManagerParser) parseCowMasterResponse(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	var success uint8
	var pid int32

	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse cowmaster port: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &success); err != nil {
		return nil, fmt.Errorf("failed to parse cowmaster success: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &pid); err != nil {
		return nil, fmt.Errorf("failed to parse cowmaster pid: %w", err)
	}

	p.logger.Info().
		Uint16("port", port).
		Bool("success", success == 1).
		Int32("pid", pid).
		Msg("cowmaster fork response")

	return &events.Event{
		Type:   events.EventCowMasterResponse,
		Source: "cowmaster",
		Payload: events.CowMasterResponsePayload{
			Port:    port,
			Success: success == 1,
			PID:     int(pid),
		},
	}, nil
}

// parseReplayStatus handles packet 0x4A: replay upload status.
func (p *GameManagerParser) parseReplayStatus(r *bytes.Reader) (*events.Event, error) {
	var port uint16
	var matchID uint32
	var status uint8

	if err := binary.Read(r, binary.LittleEndian, &port); err != nil {
		return nil, fmt.Errorf("failed to parse replay port: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &matchID); err != nil {
		return nil, fmt.Errorf("failed to parse replay match id: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &status); err != nil {
		return nil, fmt.Errorf("failed to parse replay status: %w", err)
	}

	p.logger.Debug().
		Uint16("port", port).
		Uint32("match_id", matchID).
		Uint8("status", status).
		Msg("replay status update")

	return &events.Event{
		Type:   events.EventReplayStatus,
		Source: fmt.Sprintf("game_server:%d", port),
		Payload: events.ReplayStatusPayload{
			Port:    port,
			MatchID: matchID,
			Status:  events.ReplayStatus(status),
		},
	}, nil
}

// readString reads a null-terminated or length-prefixed string from a reader.
// Format: [length:1][string bytes...]
func readString(r *bytes.Reader) (string, error) {
	var length uint8
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}

	if length == 0 {
		return "", nil
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}

	// Trim null bytes
	return string(bytes.TrimRight(buf, "\x00")), nil
}
