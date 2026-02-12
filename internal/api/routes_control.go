package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/events"
)

// handleStartServer starts a game server on the specified port.
func (s *Server) handleStartServer(c *gin.Context) {
	port, err := parsePort(c)
	if err != nil {
		return
	}

	inst, ok := s.manager.GetInstance(uint16(port))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found", "port": port})
		return
	}

	if inst.IsRunning() {
		c.JSON(http.StatusConflict, gin.H{"error": "server already running", "port": port})
		return
	}

	// Use background context — the game server must outlive the HTTP request.
	if err := inst.Start(context.Background()); err != nil {
		log.Error().Err(err).Uint16("port", uint16(port)).Msg("API: failed to start server")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	username, _ := c.Get("discord_username")
	log.Info().
		Uint16("port", uint16(port)).
		Interface("user", username).
		Msg("API: server started")

	c.JSON(http.StatusOK, gin.H{
		"status":  "started",
		"port":    port,
	})
}

// handleStopServer stops a game server on the specified port.
func (s *Server) handleStopServer(c *gin.Context) {
	port, err := parsePort(c)
	if err != nil {
		return
	}

	inst, ok := s.manager.GetInstance(uint16(port))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found", "port": port})
		return
	}

	if !inst.IsRunning() {
		c.JSON(http.StatusConflict, gin.H{"error": "server not running", "port": port})
		return
	}

	if err := inst.Stop(); err != nil {
		log.Error().Err(err).Uint16("port", uint16(port)).Msg("API: failed to stop server")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	username, _ := c.Get("discord_username")
	log.Info().
		Uint16("port", uint16(port)).
		Interface("user", username).
		Msg("API: server stopped")

	c.JSON(http.StatusOK, gin.H{
		"status": "stopped",
		"port":   port,
	})
}

// handleEnableServer enables a game server (allows it to start).
func (s *Server) handleEnableServer(c *gin.Context) {
	port, err := parsePort(c)
	if err != nil {
		return
	}

	inst, ok := s.manager.GetInstance(uint16(port))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found", "port": port})
		return
	}

	inst.Enable()

	c.JSON(http.StatusOK, gin.H{
		"status":  "enabled",
		"port":    port,
	})
}

// handleDisableServer disables a game server (prevents it from starting).
func (s *Server) handleDisableServer(c *gin.Context) {
	port, err := parsePort(c)
	if err != nil {
		return
	}

	inst, ok := s.manager.GetInstance(uint16(port))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found", "port": port})
		return
	}

	inst.Disable()

	// Also stop if currently running
	if inst.IsRunning() {
		inst.Stop()
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "disabled",
		"port":   port,
	})
}

// handleRestartServer restarts a game server.
func (s *Server) handleRestartServer(c *gin.Context) {
	port, err := parsePort(c)
	if err != nil {
		return
	}

	inst, ok := s.manager.GetInstance(uint16(port))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found", "port": port})
		return
	}

	// Use background context — the restart (stop + start) must outlive the HTTP request.
	go func() {
		if err := inst.Restart(context.Background()); err != nil {
			log.Error().Err(err).Uint16("port", uint16(port)).Msg("API: restart failed")
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"status": "restarting",
		"port":   port,
	})
}

// handleMessageServer sends an in-game message to a server.
func (s *Server) handleMessageServer(c *gin.Context) {
	port, err := parsePort(c)
	if err != nil {
		return
	}

	var body struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	s.eventBus.Emit(c.Request.Context(), events.Event{
		Type:   events.EventMessageServer,
		Source: "api",
		Payload: events.ServerCommandPayload{
			Port: uint16(port),
			Args: []string{body.Message},
		},
	})

	c.JSON(http.StatusOK, gin.H{
		"status":  "message_sent",
		"port":    port,
		"message": body.Message,
	})
}

// handleStartAll starts all configured game server instances (one-click).
func (s *Server) handleStartAll(c *gin.Context) {
	// Use background context — servers must outlive the HTTP request.
	if err := s.manager.StartAll(context.Background()); err != nil {
		log.Warn().Err(err).Msg("API: start all had failures")
		c.JSON(http.StatusOK, gin.H{
			"status": "started_with_errors",
			"error":  err.Error(),
		})
		return
	}
	username, _ := c.Get("discord_username")
	log.Info().Interface("user", username).Msg("API: start all servers")
	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

// handleStopAll stops all running game server instances (one-click).
func (s *Server) handleStopAll(c *gin.Context) {
	s.manager.StopAll()
	username, _ := c.Get("discord_username")
	log.Info().Interface("user", username).Msg("API: stop all servers")
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// handleRestartAll stops all game servers then starts them again (one-click).
func (s *Server) handleRestartAll(c *gin.Context) {
	s.manager.StopAll()
	// Use background context — servers must outlive the HTTP request.
	if err := s.manager.StartAll(context.Background()); err != nil {
		log.Warn().Err(err).Msg("API: restart all had failures")
		c.JSON(http.StatusOK, gin.H{
			"status": "restarted_with_errors",
			"error":  err.Error(),
		})
		return
	}
	username, _ := c.Get("discord_username")
	log.Info().Interface("user", username).Msg("API: restart all servers")
	c.JSON(http.StatusOK, gin.H{"status": "restarted"})
}

// parsePort extracts and validates the port parameter from the URL.
func parsePort(c *gin.Context) (uint64, error) {
	portStr := c.Param("port")
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port number"})
		return 0, err
	}
	return port, nil
}
