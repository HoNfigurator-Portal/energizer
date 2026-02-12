package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ChatServerParser parses binary packets for the Manager <-> Chat Server protocol.
// This replaces the Python ManagerChatParser class.
type ChatServerParser struct {
	logger zerolog.Logger
}

// NewChatServerParser creates a new parser for chat server protocol.
func NewChatServerParser() *ChatServerParser {
	return &ChatServerParser{
		logger: log.With().Str("component", "chat_parser").Logger(),
	}
}

// ReadChatPacket reads a chat server protocol packet.
// Chat packets have: [2-byte LE length][2-byte LE command][payload...]
func ReadChatPacket(r io.Reader) (*ChatPacket, error) {
	var length uint16
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read chat packet length: %w", err)
	}

	if length < 2 {
		return nil, fmt.Errorf("chat packet too small: %d bytes", length)
	}

	if length > MaxPacketSize {
		return nil, fmt.Errorf("chat packet too large: %d bytes", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("failed to read chat packet data: %w", err)
	}

	cmd := binary.LittleEndian.Uint16(data[:2])

	return &ChatPacket{
		Command: cmd,
		Payload: data[2:],
	}, nil
}

// WriteChatPacket writes a chat server protocol packet.
func WriteChatPacket(w io.Writer, cmd uint16, payload []byte) error {
	totalLen := uint16(2 + len(payload)) // 2 bytes for command + payload

	if err := binary.Write(w, binary.LittleEndian, totalLen); err != nil {
		return fmt.Errorf("failed to write chat packet length: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, cmd); err != nil {
		return fmt.Errorf("failed to write chat packet command: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("failed to write chat packet payload: %w", err)
		}
	}
	return nil
}

// ParseChatPacket processes a chat packet and returns its type and parsed data.
func (p *ChatServerParser) ParseChatPacket(pkt *ChatPacket) (interface{}, error) {
	r := bytes.NewReader(pkt.Payload)

	switch pkt.Command {
	case PktChatShutdown:
		return p.parseShutdown(r)
	case PktChatReplayReq:
		return p.parseReplayRequest(r)
	case PktChatKeepAlive:
		return &ChatKeepAlive{}, nil
	default:
		p.logger.Debug().
			Uint16("command", pkt.Command).
			Int("payload_len", len(pkt.Payload)).
			Msg("unhandled chat packet")
		return nil, nil
	}
}

// ChatShutdown represents a shutdown notice from the chat server.
type ChatShutdown struct {
	Reason string
}

// ChatKeepAlive represents a keepalive response.
type ChatKeepAlive struct{}

// ChatReplayRequest represents a replay request from a player.
type ChatReplayRequest struct {
	MatchID   uint32
	AccountID uint32
}

func (p *ChatServerParser) parseShutdown(r *bytes.Reader) (*ChatShutdown, error) {
	reason, _ := readChatString(r)
	p.logger.Warn().Str("reason", reason).Msg("chat server shutdown notice")
	return &ChatShutdown{Reason: reason}, nil
}

func (p *ChatServerParser) parseReplayRequest(r *bytes.Reader) (*ChatReplayRequest, error) {
	var matchID, accountID uint32

	if err := binary.Read(r, binary.LittleEndian, &matchID); err != nil {
		return nil, fmt.Errorf("failed to parse replay request match id: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &accountID); err != nil {
		return nil, fmt.Errorf("failed to parse replay request account id: %w", err)
	}

	p.logger.Info().
		Uint32("match_id", matchID).
		Uint32("account_id", accountID).
		Msg("replay request received")

	return &ChatReplayRequest{
		MatchID:   matchID,
		AccountID: accountID,
	}, nil
}

// readChatString reads a null-terminated string from a reader.
func readChatString(r *bytes.Reader) (string, error) {
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return buf.String(), err
		}
		if b == 0 {
			break
		}
		buf.WriteByte(b)
	}
	return buf.String(), nil
}
