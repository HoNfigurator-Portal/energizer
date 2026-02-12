package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// PacketBuilder constructs binary packets for sending to game servers and chat servers.
type PacketBuilder struct {
	buf bytes.Buffer
}

// NewPacketBuilder creates a new PacketBuilder.
func NewPacketBuilder() *PacketBuilder {
	return &PacketBuilder{}
}

// Reset clears the builder for reuse.
func (b *PacketBuilder) Reset() {
	b.buf.Reset()
}

// WriteByte writes a single byte.
func (b *PacketBuilder) WriteByte(v byte) *PacketBuilder {
	b.buf.WriteByte(v)
	return b
}

// WriteUint16 writes a uint16 in little-endian order.
func (b *PacketBuilder) WriteUint16(v uint16) *PacketBuilder {
	binary.Write(&b.buf, binary.LittleEndian, v)
	return b
}

// WriteUint32 writes a uint32 in little-endian order.
func (b *PacketBuilder) WriteUint32(v uint32) *PacketBuilder {
	binary.Write(&b.buf, binary.LittleEndian, v)
	return b
}

// WriteInt32 writes an int32 in little-endian order.
func (b *PacketBuilder) WriteInt32(v int32) *PacketBuilder {
	binary.Write(&b.buf, binary.LittleEndian, v)
	return b
}

// WriteFloat32 writes a float32 in little-endian order.
func (b *PacketBuilder) WriteFloat32(v float32) *PacketBuilder {
	binary.Write(&b.buf, binary.LittleEndian, v)
	return b
}

// WriteString writes a length-prefixed string.
// Format: [length:1][string bytes...]
func (b *PacketBuilder) WriteString(s string) *PacketBuilder {
	data := []byte(s)
	if len(data) > 255 {
		data = data[:255]
	}
	b.buf.WriteByte(byte(len(data)))
	b.buf.Write(data)
	return b
}

// WriteNullString writes a null-terminated string.
func (b *PacketBuilder) WriteNullString(s string) *PacketBuilder {
	b.buf.WriteString(s)
	b.buf.WriteByte(0)
	return b
}

// WriteBytes writes raw bytes.
func (b *PacketBuilder) WriteBytes(data []byte) *PacketBuilder {
	b.buf.Write(data)
	return b
}

// Build returns the constructed packet bytes.
func (b *PacketBuilder) Build() []byte {
	return b.buf.Bytes()
}

// BuildWithLength returns the packet with a 2-byte LE length prefix.
func (b *PacketBuilder) BuildWithLength() []byte {
	data := b.buf.Bytes()
	result := make([]byte, 2+len(data))
	binary.LittleEndian.PutUint16(result[:2], uint16(len(data)))
	copy(result[2:], data)
	return result
}

// Len returns the current size of the packet being built.
func (b *PacketBuilder) Len() int {
	return b.buf.Len()
}

// ---- Pre-built packet constructors ----

// BuildChatHandshake creates a chat server handshake packet (0x1600).
// Format: [cmd:2][session_cookie:null_str][server_id:4]
func BuildChatHandshake(sessionCookie string, serverID uint32) []byte {
	b := NewPacketBuilder()
	b.WriteNullString(sessionCookie)
	b.WriteUint32(serverID)
	return b.Build()
}

// BuildChatServerInfo creates a server info packet (0x1602).
// Format: [cmd:2][region:null_str][ip:null_str][name:null_str][version:null_str]
func BuildChatServerInfo(region, ip, name, version string) []byte {
	b := NewPacketBuilder()
	b.WriteNullString(region)
	b.WriteNullString(ip)
	b.WriteNullString(name)
	b.WriteNullString(version)
	return b.Build()
}

// BuildChatReplayStatus creates a replay status update packet (0x1603).
// Format: [cmd:2][match_id:4][status:1]
func BuildChatReplayStatus(matchID uint32, status byte) []byte {
	b := NewPacketBuilder()
	b.WriteUint32(matchID)
	b.WriteByte(status)
	return b.Build()
}

// BuildManagerCommand creates a command packet to send to a game server.
func BuildManagerCommand(command string) []byte {
	b := NewPacketBuilder()
	b.WriteByte(PktManagerCommand)
	b.WriteNullString(command)
	return b.Build()
}

// BuildManagerKick creates a kick packet for a game server.
func BuildManagerKick(playerID uint32, reason string) []byte {
	b := NewPacketBuilder()
	b.WriteByte(PktManagerKick)
	b.WriteUint32(playerID)
	b.WriteNullString(reason)
	return b.Build()
}

// BuildManagerMessage creates an in-game message packet.
func BuildManagerMessage(message string) []byte {
	b := NewPacketBuilder()
	b.WriteByte(PktManagerMessage)
	b.WriteNullString(message)
	return b.Build()
}

// BuildAutoPingResponse creates a UDP auto-ping response.
// Format: [magic:1][server_name:null_str][version:null_str]
func BuildAutoPingResponse(serverName, version string) []byte {
	b := NewPacketBuilder()
	b.WriteByte(AutoPingMagicByte)
	b.WriteNullString(serverName)
	b.WriteNullString(version)
	return b.Build()
}

// String returns a hex dump of the current packet for debugging.
func (b *PacketBuilder) String() string {
	data := b.buf.Bytes()
	return fmt.Sprintf("PacketBuilder[%d bytes]: %x", len(data), data)
}
