// Package protocol implements the binary protocol parsers and builders
// for communication between Energizer and HoN game servers, chat servers,
// and the master server. All packets use little-endian byte order with
// a 2-byte length prefix.
package protocol

// Packet command bytes for Game Server <-> Manager communication.
const (
	// Incoming from game server
	PktServerAnnounce    byte = 0x40 // Server hello with port
	PktServerClosed      byte = 0x41 // Server shutting down
	PktServerStatus      byte = 0x42 // Telemetry: uptime, CPU, players, phase
	PktLongFrame         byte = 0x43 // Lag / long frame detection
	PktLobbyCreated      byte = 0x44 // Match lobby created (matchID, map, mode)
	PktLobbyClosed       byte = 0x45 // Match lobby closed
	PktPlayerConnection  byte = 0x47 // Player connected/disconnected
	PktCowMasterResponse byte = 0x49 // CowMaster fork response
	PktReplayStatus      byte = 0x4A // Replay upload status update

	// Outgoing to game server
	PktManagerCommand byte = 0x50 // Send command to game server
	PktManagerKick    byte = 0x51 // Kick a player
	PktManagerMessage byte = 0x52 // Send in-game message
)

// Chat server protocol command bytes (Manager <-> Chat Server).
const (
	PktChatHandshake    uint16 = 0x1600 // Handshake with session + server ID
	PktChatServerInfo   uint16 = 0x1602 // Server info (region, IP, name, version)
	PktChatReplayStatus uint16 = 0x1603 // Replay status update to chat server
	PktChatShutdown     uint16 = 0x0400 // Shutdown notice
	PktChatKeepAlive    uint16 = 0x0200 // Keepalive heartbeat
	PktChatReplayReq    uint16 = 0x1704 // Replay request from player
)

// MaxPacketSize is the maximum allowed size for a single packet.
const MaxPacketSize = 65535

// LengthPrefixSize is the size of the length prefix in bytes.
const LengthPrefixSize = 2

// Packet represents a raw binary packet with command and payload.
type Packet struct {
	Command byte
	Payload []byte
}

// ChatPacket represents a chat server protocol packet with a 2-byte command.
type ChatPacket struct {
	Command uint16
	Payload []byte
}

// AutoPingMagicByte is the magic byte used in UDP auto-ping probes.
const AutoPingMagicByte byte = 0xCA
