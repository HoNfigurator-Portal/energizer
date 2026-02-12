// Package match implements the HoN match log parser for extracting
// player connections, chat messages, and match metadata from game
// server log files.
package match

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Parser reads and parses HoN game server match log files.
// Log files are typically UTF-16 LE encoded in the original format.
type Parser struct{}

// NewParser creates a new match log parser.
func NewParser() *Parser {
	return &Parser{}
}

// PlayerConnection represents a player join/leave event from the logs.
type PlayerConnection struct {
	Timestamp  time.Time `json:"timestamp"`
	PlayerName string    `json:"player_name"`
	PlayerID   uint32    `json:"player_id"`
	PSR        float64   `json:"psr"`
	Connected  bool      `json:"connected"`
}

// ChatMessage represents a chat message from the match logs.
type ChatMessage struct {
	Timestamp  time.Time `json:"timestamp"`
	PlayerName string    `json:"player_name"`
	Message    string    `json:"message"`
	Channel    string    `json:"channel"` // "all", "team", "lobby"
}

// MatchInfo contains parsed metadata about a match.
type MatchInfo struct {
	MatchID     uint32             `json:"match_id"`
	MapName     string             `json:"map_name"`
	GameMode    string             `json:"game_mode"`
	StartTime   time.Time          `json:"start_time"`
	EndTime     time.Time          `json:"end_time"`
	Players     []PlayerConnection `json:"players"`
	ChatLog     []ChatMessage      `json:"chat_log"`
}

// Regex patterns for parsing log lines
var (
	rePlayerConnect = regexp.MustCompile(
		`\[(\d{2}:\d{2}:\d{2})\]\s+Player\s+(\S+)\s+\(ID:\s*(\d+)(?:,\s*PSR:\s*([\d.]+))?\)\s+connected`)
	rePlayerDisconnect = regexp.MustCompile(
		`\[(\d{2}:\d{2}:\d{2})\]\s+Player\s+(\S+)\s+disconnected`)
	reChatMessage = regexp.MustCompile(
		`\[(\d{2}:\d{2}:\d{2})\]\s+\[(\w+)\]\s+(\S+):\s+(.+)`)
	reMatchID = regexp.MustCompile(
		`Match\s+ID:\s*(\d+)`)
	reMapName = regexp.MustCompile(
		`Map:\s+(\S+)`)
	reGameMode = regexp.MustCompile(
		`Game\s+Mode:\s+(\S+)`)
)

// ParseFile reads and parses a match log file.
func (p *Parser) ParseFile(filePath string) (*MatchInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", filePath, err)
	}
	defer file.Close()

	info := &MatchInfo{
		Players: make([]PlayerConnection, 0),
		ChatLog: make([]ChatMessage, 0),
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Clean potential UTF-16 artifacts
		line = cleanLine(line)

		// Try to match each pattern
		p.parseLine(line, info)
	}

	if err := scanner.Err(); err != nil {
		log.Warn().Err(err).Str("file", filePath).Msg("error reading log file")
	}

	log.Info().
		Str("file", filePath).
		Uint32("match_id", info.MatchID).
		Int("players", len(info.Players)).
		Int("chat_lines", len(info.ChatLog)).
		Int("lines_parsed", lineNum).
		Msg("match log parsed")

	return info, nil
}

// parseLine processes a single line from the log file.
func (p *Parser) parseLine(line string, info *MatchInfo) {
	// Match ID
	if matches := reMatchID.FindStringSubmatch(line); len(matches) > 1 {
		fmt.Sscanf(matches[1], "%d", &info.MatchID)
		return
	}

	// Map name
	if matches := reMapName.FindStringSubmatch(line); len(matches) > 1 {
		info.MapName = matches[1]
		return
	}

	// Game mode
	if matches := reGameMode.FindStringSubmatch(line); len(matches) > 1 {
		info.GameMode = matches[1]
		return
	}

	// Player connect
	if matches := rePlayerConnect.FindStringSubmatch(line); len(matches) > 2 {
		conn := PlayerConnection{
			Timestamp:  parseTimestamp(matches[1]),
			PlayerName: matches[2],
			Connected:  true,
		}
		if len(matches) > 3 {
			fmt.Sscanf(matches[3], "%d", &conn.PlayerID)
		}
		if len(matches) > 4 {
			fmt.Sscanf(matches[4], "%f", &conn.PSR)
		}
		info.Players = append(info.Players, conn)
		return
	}

	// Player disconnect
	if matches := rePlayerDisconnect.FindStringSubmatch(line); len(matches) > 2 {
		conn := PlayerConnection{
			Timestamp:  parseTimestamp(matches[1]),
			PlayerName: matches[2],
			Connected:  false,
		}
		info.Players = append(info.Players, conn)
		return
	}

	// Chat message
	if matches := reChatMessage.FindStringSubmatch(line); len(matches) > 4 {
		msg := ChatMessage{
			Timestamp:  parseTimestamp(matches[1]),
			Channel:    strings.ToLower(matches[2]),
			PlayerName: matches[3],
			Message:    matches[4],
		}
		info.ChatLog = append(info.ChatLog, msg)
		return
	}
}

// parseTimestamp parses a HH:MM:SS timestamp.
func parseTimestamp(ts string) time.Time {
	t, err := time.Parse("15:04:05", ts)
	if err != nil {
		return time.Time{}
	}
	// Set to today's date
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(),
		t.Hour(), t.Minute(), t.Second(), 0, now.Location())
}

// cleanLine removes UTF-16 BOM and null bytes from a line.
func cleanLine(line string) string {
	// Remove BOM
	line = strings.TrimPrefix(line, "\xef\xbb\xbf")
	line = strings.TrimPrefix(line, "\xff\xfe")

	// Remove null bytes (from UTF-16 LE)
	line = strings.ReplaceAll(line, "\x00", "")

	return strings.TrimSpace(line)
}
