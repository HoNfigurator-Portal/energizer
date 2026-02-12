package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
)

// handleGetConfig returns the full current configuration.
func (s *Server) handleGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"hon_data":         s.cfg.GetHoNData(),
		"application_data": s.cfg.GetApplicationData(),
	})
}

// handleSetHoNData updates HoN server configuration.
func (s *Server) handleSetHoNData(c *gin.Context) {
	var honData config.HoNData
	if err := c.ShouldBindJSON(&honData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.cfg.SetHoNData(honData)

	if err := s.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}

	// Emit config changed event
	s.eventBus.Emit(c.Request.Context(), events.Event{
		Type:   events.EventConfigChanged,
		Source: "api",
		Payload: events.ConfigChangedPayload{
			Section: "hon_data",
		},
	})

	username, _ := c.Get("discord_username")
	log.Info().Interface("user", username).Msg("API: HoN data updated")

	c.JSON(http.StatusOK, gin.H{
		"status": "updated",
		"data":   s.cfg.GetHoNData(),
	})
}

// handleSetAppData updates application configuration.
func (s *Server) handleSetAppData(c *gin.Context) {
	var appData config.ApplicationData
	if err := c.ShouldBindJSON(&appData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.cfg.SetApplicationData(appData)

	if err := s.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}

	s.eventBus.Emit(c.Request.Context(), events.Event{
		Type:   events.EventConfigChanged,
		Source: "api",
		Payload: events.ConfigChangedPayload{
			Section: "application_data",
		},
	})

	c.JSON(http.StatusOK, gin.H{
		"status": "updated",
	})
}

// handleAddServers dynamically adds new game servers.
func (s *Server) handleAddServers(c *gin.Context) {
	var body struct {
		Count int `json:"count" binding:"required,min=1,max=20"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.manager.AddServers(c.Request.Context(), body.Count); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":        "added",
		"count":         body.Count,
		"total_servers": s.manager.GetTotalServers(),
	})
}

// handleRemoveServers removes game servers from the pool.
func (s *Server) handleRemoveServers(c *gin.Context) {
	var body struct {
		Ports []uint16 `json:"ports" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.manager.RemoveServers(body.Ports); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":        "removed",
		"ports":         body.Ports,
		"total_servers": s.manager.GetTotalServers(),
	})
}

// handleGetUsers returns all registered users.
func (s *Server) handleGetUsers(c *gin.Context) {
	users, err := s.rolesDB.GetAllUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// handleCreateUser creates a new user.
func (s *Server) handleCreateUser(c *gin.Context) {
	var body struct {
		DiscordID string `json:"discord_id" binding:"required"`
		Username  string `json:"username" binding:"required"`
		Role      string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if body.Role == "" {
		body.Role = "user"
	}

	if err := s.rolesDB.CreateUser(body.DiscordID, body.Username, body.Role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":     "created",
		"discord_id": body.DiscordID,
		"role":       body.Role,
	})
}

// handleDeleteUser deletes a user.
func (s *Server) handleDeleteUser(c *gin.Context) {
	discordID := c.Param("discord_id")

	if err := s.rolesDB.DeleteUser(discordID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "deleted",
		"discord_id": discordID,
	})
}

// handleGetRoles returns all available roles.
func (s *Server) handleGetRoles(c *gin.Context) {
	roles, err := s.rolesDB.GetAllRoles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

// handleAssignRole assigns a role to a user.
func (s *Server) handleAssignRole(c *gin.Context) {
	discordID := c.Param("discord_id")

	var body struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.rolesDB.AssignRole(discordID, body.Role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "assigned",
		"discord_id": discordID,
		"role":       body.Role,
	})
}

// handleRemoveRole removes a role from a user.
func (s *Server) handleRemoveRole(c *gin.Context) {
	discordID := c.Param("discord_id")
	role := c.Param("role")

	if err := s.rolesDB.RemoveRole(discordID, role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "removed",
		"discord_id": discordID,
		"role":       role,
	})
}
