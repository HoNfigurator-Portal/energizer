package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/energizer-project/energizer/internal/util"
)

// handleGetInstancesStatus returns status of all game server instances.
func (s *Server) handleGetInstancesStatus(c *gin.Context) {
	instances := s.manager.GetAllInfo()
	c.JSON(http.StatusOK, gin.H{
		"instances": instances,
		"total":     len(instances),
	})
}

// handleGetTotalServers returns the total server count.
func (s *Server) handleGetTotalServers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"total":    s.manager.GetTotalServers(),
		"running":  s.manager.GetRunningCount(),
		"occupied": s.manager.GetOccupiedCount(),
	})
}

// handleGetCPUUsage returns current system CPU usage.
func (s *Server) handleGetCPUUsage(c *gin.Context) {
	usage, err := util.GetCPUUsage()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cpu_percent": usage,
	})
}

// handleGetMemoryUsage returns current system memory usage.
func (s *Server) handleGetMemoryUsage(c *gin.Context) {
	mem, err := util.GetMemoryUsage()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total_mb":     mem.Total,
		"used_mb":      mem.Used,
		"available_mb": mem.Available,
		"used_percent": mem.UsedPercent,
	})
}

// handleGetReplay serves a replay file for download.
func (s *Server) handleGetReplay(c *gin.Context) {
	matchIDStr := c.Param("match_id")
	matchID, err := strconv.ParseUint(matchIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid match ID"})
		return
	}

	honData := s.cfg.GetHoNData()
	replayDir := filepath.Join(honData.HomeDirectory, "replays")
	replayFile := filepath.Join(replayDir, "M"+matchIDStr+".honreplay")

	// Security: prevent path traversal
	absPath, err := filepath.Abs(replayFile)
	if err != nil || !strings.HasPrefix(absPath, filepath.Clean(replayDir)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if _, err := os.Stat(replayFile); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "replay not found",
			"match_id": matchID,
		})
		return
	}

	c.Header("Content-Disposition",
		"attachment; filename=M"+matchIDStr+".honreplay")
	c.File(replayFile)
}

// handleGetLogEntries returns recent log entries.
func (s *Server) handleGetLogEntries(c *gin.Context) {
	countStr := c.DefaultQuery("count", "100")
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 {
		count = 100
	}
	if count > 1000 {
		count = 1000
	}

	logDir := s.cfg.ApplicationData.Logging.Directory
	entries, err := readRecentLogEntries(logDir, count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"count":   len(entries),
	})
}

// handleGetTasksStatus returns the status of background tasks.
func (s *Server) handleGetTasksStatus(c *gin.Context) {
	// Return basic task status (health checks, scheduler, etc.)
	c.JSON(http.StatusOK, gin.H{
		"tasks": []gin.H{
			{"name": "health_checks", "status": "running"},
			{"name": "scheduler", "status": "running"},
			{"name": "mqtt_telemetry", "status": "running"},
		},
	})
}

// logEntry is a parsed log entry for the API response.
type logEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// readRecentLogEntries reads and parses the most recent log entries from log files.
// Zerolog writes JSON lines; we parse them into structured objects for the dashboard.
func readRecentLogEntries(logDir string, count int) ([]logEntry, error) {
	dirEntries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, err
	}

	if len(dirEntries) == 0 {
		return []logEntry{}, nil
	}

	// Find the most recent log file
	var latestFile string
	for i := len(dirEntries) - 1; i >= 0; i-- {
		if !dirEntries[i].IsDir() && filepath.Ext(dirEntries[i].Name()) == ".log" {
			latestFile = filepath.Join(logDir, dirEntries[i].Name())
			break
		}
	}

	if latestFile == "" {
		return []logEntry{}, nil
	}

	// Read file content
	data, err := os.ReadFile(latestFile)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")

	// Take last N lines
	start := len(lines) - count
	if start < 0 {
		start = 0
	}

	// Known zerolog internal fields to exclude from "fields"
	knownKeys := map[string]bool{
		"level": true, "time": true, "message": true,
		"caller": true, "app": true,
	}

	result := make([]logEntry, 0, count)
	for _, line := range lines[start:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse the JSON line
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			// Not valid JSON — include as a plain message
			result = append(result, logEntry{Message: line})
			continue
		}

		entry := logEntry{
			Level:   stringFromMap(raw, "level"),
			Message: stringFromMap(raw, "message"),
		}

		// Parse timestamp (zerolog uses "time" field)
		if t, ok := raw["time"]; ok {
			entry.Timestamp = fmt.Sprintf("%v", t)
		}

		// Collect remaining fields
		extra := make(map[string]interface{})
		for k, v := range raw {
			if !knownKeys[k] {
				extra[k] = v
			}
		}
		if len(extra) > 0 {
			entry.Fields = extra
		}

		result = append(result, entry)
	}

	return result, nil
}

// stringFromMap extracts a string value from a map, returning "" if missing.
func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
