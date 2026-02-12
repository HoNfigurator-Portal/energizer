package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
)

const (
	discordAPIURL      = "https://discord.com/api/v10"
	managementAPIURL   = "https://management.honfigurator.app:3001/api-ui/sendDiscordMessage"
	discordTokenCache  = 20 * time.Minute
)

// DiscordConnector handles Discord-related communication:
// - OAuth2 token verification for API authentication
// - Admin notifications via the management website API
// - Webhook notifications for server events
type DiscordConnector struct {
	mu sync.RWMutex

	cfg      *config.Config
	eventBus *events.EventBus
	client   *http.Client

	// Token verification cache
	tokenCache map[string]*cachedToken
}

type cachedToken struct {
	userID    string
	username  string
	expiresAt time.Time
}

// DiscordUser represents a Discord user from the OAuth2 API.
type DiscordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Avatar        string `json:"avatar"`
}

// NewDiscordConnector creates a new Discord connector.
func NewDiscordConnector(cfg *config.Config, eventBus *events.EventBus) *DiscordConnector {
	dc := &DiscordConnector{
		cfg:        cfg,
		eventBus:   eventBus,
		client:     &http.Client{Timeout: 10 * time.Second},
		tokenCache: make(map[string]*cachedToken),
	}

	// Subscribe to notification events
	eventBus.Subscribe(events.EventNotifyDiscordAdmin, "discord.notify", dc.onNotifyAdmin)

	return dc
}

// VerifyToken verifies a Discord OAuth2 bearer token and returns the user info.
// Results are cached for 20 minutes to reduce API calls (matching original behavior).
func (dc *DiscordConnector) VerifyToken(ctx context.Context, token string) (*DiscordUser, error) {
	// Check cache first
	dc.mu.RLock()
	cached, ok := dc.tokenCache[token]
	dc.mu.RUnlock()

	if ok && time.Now().Before(cached.expiresAt) {
		return &DiscordUser{
			ID:       cached.userID,
			Username: cached.username,
		}, nil
	}

	// Verify with Discord API
	req, err := http.NewRequestWithContext(ctx, "GET", discordAPIURL+"/users/@me", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := dc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Discord API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Remove from cache
		dc.mu.Lock()
		delete(dc.tokenCache, token)
		dc.mu.Unlock()
		return nil, fmt.Errorf("invalid or expired Discord token")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Discord API returned status %d: %s", resp.StatusCode, string(body))
	}

	var user DiscordUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode Discord user: %w", err)
	}

	// Cache the result
	dc.mu.Lock()
	dc.tokenCache[token] = &cachedToken{
		userID:    user.ID,
		username:  user.Username,
		expiresAt: time.Now().Add(discordTokenCache),
	}
	dc.mu.Unlock()

	return &user, nil
}

// SendAdminNotification sends a notification to the server admin via Discord.
func (dc *DiscordConnector) SendAdminNotification(ctx context.Context, title, message, level string) error {
	discordCfg := dc.cfg.ApplicationData.Discord

	// Try webhook first
	if discordCfg.WebhookURL != "" {
		return dc.sendWebhook(ctx, discordCfg.WebhookURL, title, message, level)
	}

	// Fall back to management API
	if discordCfg.OwnerID != "" {
		return dc.sendViaManagementAPI(ctx, discordCfg.OwnerID, title, message, level)
	}

	log.Warn().Msg("no Discord notification method configured")
	return nil
}

// sendWebhook sends a notification via Discord webhook.
func (dc *DiscordConnector) sendWebhook(ctx context.Context, webhookURL, title, message, level string) error {
	// Color based on level
	var color int
	switch level {
	case "error":
		color = 0xFF0000 // Red
	case "warning":
		color = 0xFFAA00 // Orange
	default:
		color = 0x00FF00 // Green
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": message,
				"color":       color,
				"timestamp":   time.Now().Format(time.RFC3339),
				"footer": map[string]string{
					"text": "Energizer Server Manager",
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := dc.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Debug().Str("title", title).Msg("Discord webhook notification sent")
	return nil
}

// sendViaManagementAPI sends a notification through the HoNfigurator management website.
func (dc *DiscordConnector) sendViaManagementAPI(ctx context.Context, ownerID, title, message, level string) error {
	payload := map[string]string{
		"discord_id": ownerID,
		"title":      title,
		"message":    message,
		"level":      level,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", managementAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := dc.client.Do(req)
	if err != nil {
		return fmt.Errorf("management API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("management API returned status %d", resp.StatusCode)
	}

	log.Debug().Str("title", title).Msg("Discord notification sent via management API")
	return nil
}

// onNotifyAdmin handles EventNotifyDiscordAdmin events.
func (dc *DiscordConnector) onNotifyAdmin(ctx context.Context, event events.Event) error {
	payload, ok := event.Payload.(events.NotifyDiscordPayload)
	if !ok {
		return nil
	}

	return dc.SendAdminNotification(ctx, payload.Title, payload.Message, payload.Level)
}

// CleanExpiredCache removes expired entries from the token cache.
func (dc *DiscordConnector) CleanExpiredCache() {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	now := time.Now()
	for token, cached := range dc.tokenCache {
		if now.After(cached.expiresAt) {
			delete(dc.tokenCache, token)
		}
	}
}
