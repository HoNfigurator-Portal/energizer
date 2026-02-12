// Package api implements the REST API server for Energizer, providing
// remote management capabilities with Discord OAuth2 authentication
// and role-based access control (RBAC).
package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/connector"
	"github.com/energizer-project/energizer/internal/db"
)

// Permission levels for RBAC (matching original 3-tier model).
const (
	PermMonitor   = "monitor"   // View server status, stats
	PermControl   = "control"   // Start/stop servers
	PermConfigure = "configure" // Modify configuration, manage users
)

// AuthMiddleware handles Discord OAuth2 token verification and RBAC.
type AuthMiddleware struct {
	discord  *connector.DiscordConnector
	rolesDB  *db.RolesDatabase
	cfg      *config.Config
}

// NewAuthMiddleware creates a new auth middleware.
func NewAuthMiddleware(discord *connector.DiscordConnector, rolesDB *db.RolesDatabase, cfg *config.Config) *AuthMiddleware {
	return &AuthMiddleware{
		discord: discord,
		rolesDB: rolesDB,
		cfg:     cfg,
	}
}

// RequireAuth returns a Gin middleware that verifies Discord OAuth2 tokens.
// When auth_disabled is true in config, all requests are treated as a local admin.
func (am *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bypass auth when disabled (local/dashboard mode)
		if am.cfg.ApplicationData.Security.AuthDisabled {
			c.Set("discord_user_id", "local-admin")
			c.Set("discord_username", "Local Admin")
			c.Next()
			return
		}

		token := extractBearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "missing or invalid authorization header",
			})
			c.Abort()
			return
		}

		// Verify token with Discord API (cached for 20 minutes)
		user, err := am.discord.VerifyToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			c.Abort()
			return
		}

		// Store user info in context
		c.Set("discord_user_id", user.ID)
		c.Set("discord_username", user.Username)

		c.Next()
	}
}

// RequirePermission returns a middleware that checks RBAC permissions.
// When auth_disabled is true in config, all permissions are granted.
func (am *AuthMiddleware) RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bypass permission check when auth is disabled (local/dashboard mode)
		if am.cfg.ApplicationData.Security.AuthDisabled {
			c.Next()
			return
		}

		userID, exists := c.Get("discord_user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			c.Abort()
			return
		}

		discordID := userID.(string)

		// Check if user has the required permission
		hasPermission, err := am.rolesDB.UserHasPermission(discordID, permission)
		if err != nil {
			log.Error().Err(err).Str("user", discordID).Str("perm", permission).
				Msg("permission check failed")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "permission check failed",
			})
			c.Abort()
			return
		}

		if !hasPermission {
			c.JSON(http.StatusForbidden, gin.H{
				"error":    "insufficient permissions",
				"required": permission,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// IPWhitelist returns a middleware that restricts access to whitelisted IPs.
func (am *AuthMiddleware) IPWhitelist() gin.HandlerFunc {
	whitelist := am.cfg.ApplicationData.Security.IPWhitelist

	return func(c *gin.Context) {
		if len(whitelist) == 0 {
			c.Next()
			return
		}

		clientIP := c.ClientIP()
		for _, ip := range whitelist {
			if clientIP == ip {
				c.Next()
				return
			}
			// Check CIDR
			if _, cidr, err := net.ParseCIDR(ip); err == nil {
				if cidr.Contains(net.ParseIP(clientIP)) {
					c.Next()
					return
				}
			}
		}

		c.JSON(http.StatusForbidden, gin.H{
			"error": "access denied: IP not whitelisted",
		})
		c.Abort()
	}
}

// RateLimiter implements a simple token bucket rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	rate    int
	burst   int
}

type clientBucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a rate limiter with the specified requests per second.
func NewRateLimiter(rps int) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*clientBucket),
		rate:    rps,
		burst:   rps * 2, // Allow burst of 2x rate
	}
}

// Middleware returns a Gin middleware that rate limits by client IP.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl.rate <= 0 {
			c.Next()
			return
		}

		clientIP := c.ClientIP()

		rl.mu.Lock()
		bucket, exists := rl.clients[clientIP]
		if !exists {
			bucket = &clientBucket{
				tokens:    float64(rl.burst),
				lastCheck: time.Now(),
			}
			rl.clients[clientIP] = bucket
		}

		// Refill tokens
		now := time.Now()
		elapsed := now.Sub(bucket.lastCheck).Seconds()
		bucket.tokens += elapsed * float64(rl.rate)
		if bucket.tokens > float64(rl.burst) {
			bucket.tokens = float64(rl.burst)
		}
		bucket.lastCheck = now

		if bucket.tokens < 1 {
			rl.mu.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		bucket.tokens--
		rl.mu.Unlock()

		c.Next()
	}
}

// SecurityHeaders adds security-related HTTP headers.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Server", "Energizer")

		// Only apply strict security headers to API routes.
		// The dashboard is a local management tool and needs permissive policies
		// for fonts, inline styles, module scripts, etc.
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("X-Frame-Options", "DENY")
			c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}

		c.Next()
	}
}

// RequestLogger logs incoming HTTP requests.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start)
		log.Debug().
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Dur("duration", duration).
			Str("client_ip", c.ClientIP()).
			Msg("api request")
	}
}

func extractBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
