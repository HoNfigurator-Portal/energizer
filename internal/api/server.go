package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/connector"
	"github.com/energizer-project/energizer/internal/db"
	"github.com/energizer-project/energizer/internal/events"
	intnet "github.com/energizer-project/energizer/internal/network"
	"github.com/energizer-project/energizer/internal/server"
)

// Server is the REST API server for Energizer.
// It replaces the FastAPI server from the original Python implementation.
type Server struct {
	cfg      *config.Config
	eventBus *events.EventBus
	manager  *server.Manager

	// Dependencies
	discord  *connector.DiscordConnector
	rolesDB  *db.RolesDatabase

	// HTTP server
	httpServer *http.Server
	router     *gin.Engine
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, eventBus *events.EventBus, manager *server.Manager) *Server {
	// Set Gin mode based on log level
	if cfg.ApplicationData.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	s := &Server{
		cfg:      cfg,
		eventBus: eventBus,
		manager:  manager,
	}

	return s
}

// SetDependencies injects runtime dependencies (called after all components are initialized).
func (s *Server) SetDependencies(discord *connector.DiscordConnector, rolesDB *db.RolesDatabase) {
	s.discord = discord
	s.rolesDB = rolesDB
}

// Start initializes and starts the API server.
func (s *Server) Start(ctx context.Context) error {
	// Initialize dependencies if not set
	if s.rolesDB == nil {
		var err error
		s.rolesDB, err = db.NewRolesDatabase("config/roles.db")
		if err != nil {
			return fmt.Errorf("failed to initialize roles database: %w", err)
		}
	}

	if s.discord == nil {
		s.discord = connector.NewDiscordConnector(s.cfg, s.eventBus)
	}

	// Build router
	s.router = s.buildRouter()

	// Create HTTP server
	addr := fmt.Sprintf(":%d", s.cfg.HoNData.APIPort)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// TLS configuration
	if s.cfg.ApplicationData.Security.TLSEnabled {
		s.httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			},
		}
	}

	// Create listener with SO_REUSEADDR for immediate rebinding after restart
	lc := intnet.ReuseAddrListenConfig()
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("API server error: %w", err)
	}

	log.Info().Str("addr", addr).Msg("REST API server starting")

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	if s.cfg.ApplicationData.Security.TLSEnabled {
		tlsListener := tls.NewListener(ln, s.httpServer.TLSConfig)
		err = s.httpServer.Serve(tlsListener)
	} else {
		err = s.httpServer.Serve(ln)
	}

	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("API server error: %w", err)
	}

	return nil
}

// buildRouter creates the Gin router with all routes and middleware.
func (s *Server) buildRouter() *gin.Engine {
	router := gin.New()

	// Global middleware
	router.Use(gin.Recovery())
	router.Use(RequestLogger())
	router.Use(SecurityHeaders())

	// CORS
	allowedOrigins := s.cfg.ApplicationData.Security.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}
	router.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false, // Must be false when AllowOrigins is "*"
		MaxAge:           12 * time.Hour,
	}))

	// Rate limiting
	rateLimiter := NewRateLimiter(s.cfg.ApplicationData.Security.RateLimitRPS)
	router.Use(rateLimiter.Middleware())

	// Auth middleware
	auth := NewAuthMiddleware(s.discord, s.rolesDB, s.cfg)

	// ---- Public endpoints (no auth required) ----
	public := router.Group("/api/public")
	{
		public.GET("/ping", s.handlePing)
		public.GET("/get_server_info", s.handleGetServerInfo)
		public.GET("/get_energizer_version", s.handleGetVersion)
		public.GET("/get_hon_version", s.handleGetHoNVersion)
		public.GET("/check_filebeat_status", s.handleCheckFilebeatStatus)
		public.GET("/get_skipped_frame_data/:port", s.handleGetSkippedFrameData)
	}

	// ---- Protected endpoints ----
	protected := router.Group("/api")
	protected.Use(auth.RequireAuth())

	// Monitor-level endpoints
	monitor := protected.Group("/monitor")
	monitor.Use(auth.RequirePermission(PermMonitor))
	{
		monitor.GET("/get_instances_status", s.handleGetInstancesStatus)
		monitor.GET("/get_total_servers", s.handleGetTotalServers)
		monitor.GET("/get_cpu_usage", s.handleGetCPUUsage)
		monitor.GET("/get_memory_usage", s.handleGetMemoryUsage)
		monitor.GET("/get_replay/:match_id", s.handleGetReplay)
		monitor.GET("/get_energizer_log_entries", s.handleGetLogEntries)
		monitor.GET("/get_tasks_status", s.handleGetTasksStatus)
	}

	// Control-level endpoints
	control := protected.Group("/control")
	control.Use(auth.RequirePermission(PermControl))
	{
		control.POST("/start_server/:port", s.handleStartServer)
		control.POST("/stop_server/:port", s.handleStopServer)
		control.POST("/enable_server/:port", s.handleEnableServer)
		control.POST("/disable_server/:port", s.handleDisableServer)
		control.POST("/restart_server/:port", s.handleRestartServer)
		control.POST("/message_server/:port", s.handleMessageServer)
	}

	// Configure-level endpoints
	configure := protected.Group("/configure")
	configure.Use(auth.RequirePermission(PermConfigure))
	{
		configure.GET("/get_config", s.handleGetConfig)
		configure.POST("/set_hon_data", s.handleSetHoNData)
		configure.POST("/set_app_data", s.handleSetAppData)
		configure.POST("/add_servers", s.handleAddServers)
		configure.POST("/remove_servers", s.handleRemoveServers)

		// User/Role management
		configure.GET("/users", s.handleGetUsers)
		configure.POST("/users", s.handleCreateUser)
		configure.DELETE("/users/:discord_id", s.handleDeleteUser)
		configure.GET("/roles", s.handleGetRoles)
		configure.POST("/users/:discord_id/roles", s.handleAssignRole)
		configure.DELETE("/users/:discord_id/roles/:role", s.handleRemoveRole)
	}

	// ---- Dashboard (SPA static files) ----
	// Serve the built dashboard from dashboard/dist/ if it exists.
	// This means running energizer.exe alone serves both API and UI on one port.
	dashboardDir := findDashboardDir()
	if dashboardDir != "" {
		log.Info().Str("path", dashboardDir).Msg("serving dashboard UI")

		// Serve static assets (JS, CSS, images, etc.)
		router.Static("/assets", filepath.Join(dashboardDir, "assets"))
		router.Static("/bg", filepath.Join(dashboardDir, "bg"))
		router.Static("/icon", filepath.Join(dashboardDir, "icon"))
		router.Static("/logo", filepath.Join(dashboardDir, "logo"))

		// Serve other static files at root (favicon, etc.)
		router.StaticFile("/vite.svg", filepath.Join(dashboardDir, "vite.svg"))

		// SPA fallback: any route that is NOT /api/* and NOT a static file
		// gets served index.html so client-side routing works.
		indexHTML := filepath.Join(dashboardDir, "index.html")
		router.NoRoute(func(c *gin.Context) {
			// Don't intercept API routes -- let them 404 normally
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
				return
			}
			c.File(indexHTML)
		})
	} else {
		log.Warn().Msg("dashboard/dist not found — dashboard UI will not be available. Run 'npm run build' in the dashboard/ directory.")
		router.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"message": "Energizer API is running. Dashboard not built yet — run 'npm run build' in the dashboard/ directory.",
			})
		})
	}

	return router
}

// findDashboardDir locates the built dashboard directory.
// It checks relative to the executable and the working directory.
func findDashboardDir() string {
	candidates := []string{}

	// Relative to the working directory
	candidates = append(candidates, filepath.Join("dashboard", "dist"))

	// Relative to the executable
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, "dashboard", "dist"))
	}

	for _, dir := range candidates {
		indexPath := filepath.Join(dir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			absDir, _ := filepath.Abs(dir)
			return absDir
		}
	}
	return ""
}

// Stop gracefully stops the API server.
func (s *Server) Stop() error {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
