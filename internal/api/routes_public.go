package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/energizer-project/energizer/internal/util"
)

// handlePing returns a simple health check response.
func (s *Server) handlePing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "energizer",
		"version": "1.0.0",
	})
}

// handleGetServerInfo returns basic server information.
func (s *Server) handleGetServerInfo(c *gin.Context) {
	honData := s.cfg.GetHoNData()
	sysInfo := util.GetSystemInfo()

	c.JSON(http.StatusOK, gin.H{
		"server_name":     honData.Name,
		"server_location": honData.Location,
		"server_region":   honData.Region,
		"total_servers":   s.manager.GetTotalServers(),
		"running_servers": s.manager.GetRunningCount(),
		"occupied_servers": s.manager.GetOccupiedCount(),
		"platform":        sysInfo.Platform,
		"cpu_model":       sysInfo.CPUModel,
		"cpu_cores":       sysInfo.CPUCores,
		"total_memory_mb": sysInfo.TotalMemory,
		"public_ip":       s.manager.GetPublicIP(),
	})
}

// handleGetVersion returns the Energizer version.
func (s *Server) handleGetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": "1.0.0",
		"name":    "Energizer",
	})
}

// handleGetHoNVersion returns the current HoN server version.
func (s *Server) handleGetHoNVersion(c *gin.Context) {
	honData := s.cfg.GetHoNData()
	c.JSON(http.StatusOK, gin.H{
		"version": honData.ServerVersion,
	})
}

// handleCheckFilebeatStatus returns the Filebeat integration status.
func (s *Server) handleCheckFilebeatStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled": s.cfg.ApplicationData.Filebeat.Enabled,
		"status":  "ok",
	})
}

// handleGetSkippedFrameData returns lag/skipped frame data for a specific server port.
func (s *Server) handleGetSkippedFrameData(c *gin.Context) {
	portStr := c.Param("port")
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
		return
	}

	inst, ok := s.manager.GetInstance(uint16(port))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	lagEvents := inst.State().GetLagEvents()
	c.JSON(http.StatusOK, gin.H{
		"port":             port,
		"total_events":     inst.State().TotalLagEvents,
		"skipped_frames":   lagEvents,
	})
}
