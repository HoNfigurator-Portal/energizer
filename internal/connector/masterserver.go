// Package connector implements external service connectors for communicating
// with the HoN master server, chat server, and Discord API.
package connector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/protocol"
)

const (
	defaultMasterServerURL = "http://api.kongor.net:55555"
	replayAuthPath         = "/server_requester.php"
	patchCheckPath         = "/patcher/patcher.php"
	userAgent              = "S2 Games/Heroes of Newerth/%s/x86_64/%s"
	authRetryInterval      = 30 * time.Second
	authMaxRetries         = 5
)

// MasterServerConnector handles HTTP communication with the HoN master server
// at api.kongor.net. It authenticates, checks patches, uploads replays,
// and submits match stats using PHP-serialized request/response format.
type MasterServerConnector struct {
	mu sync.RWMutex

	cfg      *config.Config
	eventBus *events.EventBus
	client   *http.Client

	// Auth state
	sessionCookie string
	serverID      uint32
	chatServerIP  string
	chatServerPort int
	authenticated bool
	lastAuthTime  time.Time

	// Version
	upstreamVersion string
}

// NewMasterServerConnector creates a new master server connector.
func NewMasterServerConnector(cfg *config.Config, eventBus *events.EventBus) *MasterServerConnector {
	return &MasterServerConnector{
		cfg:      cfg,
		eventBus: eventBus,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: false,
			},
		},
	}
}

// ManageConnection maintains the authentication with the master server.
// It authenticates on startup and re-authenticates if the session expires.
func (c *MasterServerConnector) ManageConnection(ctx context.Context) error {
	log.Info().Msg("connecting to master server")

	retries := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := c.authenticate(ctx); err != nil {
			retries++
			if retries >= authMaxRetries {
				return fmt.Errorf("master server authentication failed after %d retries: %w",
					authMaxRetries, err)
			}
			log.Warn().Err(err).Int("retry", retries).Msg("auth failed, retrying")
			time.Sleep(authRetryInterval)
			continue
		}

		log.Info().
			Uint32("server_id", c.serverID).
			Str("chat_server", c.chatServerIP).
			Msg("authenticated with master server")

		// Stay connected, periodically re-check
		retries = 0
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				// Periodic health check / re-auth if needed
				if !c.IsAuthenticated() {
					break // Inner loop, re-authenticate
				}
			}
		}
	}
}

// authenticate performs the initial authentication with the master server.
func (c *MasterServerConnector) authenticate(ctx context.Context) error {
	honData := c.cfg.GetHoNData()

	// Build auth request using PHP serialization
	authData := map[string]interface{}{
		"login":    honData.Login,
		"password": honData.Password,
	}

	serialized, err := protocol.PHPSerialize(authData)
	if err != nil {
		return fmt.Errorf("failed to serialize auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.getMasterServerBaseURL()+replayAuthPath,
		bytes.NewBufferString(serialized))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", fmt.Sprintf(userAgent, honData.ServerVersion, getPlatformString()))

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse PHP serialized response
	result, err := protocol.PHPUnserialize(string(body))
	if err != nil {
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	// Extract auth data
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected auth response format")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if cookie, ok := resultMap["session"]; ok {
		c.sessionCookie = fmt.Sprintf("%v", cookie)
	}
	if sid, ok := resultMap["server_id"]; ok {
		if id, ok := sid.(int); ok {
			c.serverID = uint32(id)
		}
	}
	if chatIP, ok := resultMap["chat_url"]; ok {
		c.chatServerIP = fmt.Sprintf("%v", chatIP)
	}
	if chatPort, ok := resultMap["chat_port"]; ok {
		if port, ok := chatPort.(int); ok {
			c.chatServerPort = port
		}
	}

	c.authenticated = true
	c.lastAuthTime = time.Now()

	return nil
}

// IsAuthenticated returns whether the connector is currently authenticated.
func (c *MasterServerConnector) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authenticated
}

// GetSessionCookie returns the current session cookie.
func (c *MasterServerConnector) GetSessionCookie() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionCookie
}

// GetServerID returns the server ID from authentication.
func (c *MasterServerConnector) GetServerID() uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverID
}

// GetChatServerAddr returns the chat server address and port.
func (c *MasterServerConnector) GetChatServerAddr() (string, int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.chatServerIP, c.chatServerPort
}

// CompareUpstreamPatch checks if there's a newer game version available.
func (c *MasterServerConnector) CompareUpstreamPatch(ctx context.Context) (bool, string, error) {
	honData := c.cfg.GetHoNData()

	patchData := map[string]interface{}{
		"version": honData.ServerVersion,
		"os":      getPlatformString(),
		"arch":    "x86_64",
	}

	serialized, err := protocol.PHPSerialize(patchData)
	if err != nil {
		return false, "", fmt.Errorf("failed to serialize patch request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.getMasterServerBaseURL()+patchCheckPath,
		bytes.NewBufferString(serialized))
	if err != nil {
		return false, "", fmt.Errorf("failed to create patch request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", fmt.Sprintf(userAgent, honData.ServerVersion, getPlatformString()))

	resp, err := c.client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("patch check request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", err
	}

	result, err := protocol.PHPUnserialize(string(body))
	if err != nil {
		return false, "", err
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return false, "", nil
	}

	if newVersion, ok := resultMap["latest_version"]; ok {
		versionStr := fmt.Sprintf("%v", newVersion)
		if versionStr != honData.ServerVersion {
			c.mu.Lock()
			c.upstreamVersion = versionStr
			c.mu.Unlock()
			return true, versionStr, nil
		}
	}

	return false, "", nil
}

// UploadReplay uploads a replay file to the master server.
func (c *MasterServerConnector) UploadReplay(ctx context.Context, matchID uint32, filePath string) error {
	c.mu.RLock()
	session := c.sessionCookie
	c.mu.RUnlock()

	if session == "" {
		return fmt.Errorf("not authenticated")
	}

	// Open replay file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open replay file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add session field
	writer.WriteField("session", session)
	writer.WriteField("match_id", fmt.Sprintf("%d", matchID))

	// Add replay file
	part, err := writer.CreateFormFile("replay", fmt.Sprintf("M%d.honreplay", matchID))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("failed to copy replay data: %w", err)
	}

	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.getMasterServerBaseURL()+"/replay/upload.php", body)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("replay upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("replay upload returned status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Info().Uint32("match_id", matchID).Msg("replay uploaded successfully")
	return nil
}

// SendStatsFile submits match statistics to the master server.
func (c *MasterServerConnector) SendStatsFile(ctx context.Context, statsData []byte) error {
	c.mu.RLock()
	session := c.sessionCookie
	c.mu.RUnlock()

	if session == "" {
		return fmt.Errorf("not authenticated")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("session", session)

	part, err := writer.CreateFormFile("stats", "match.stats")
	if err != nil {
		return err
	}
	part.Write(statsData)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.getMasterServerBaseURL()+"/stats/submit.php", body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stats submission failed with status %d", resp.StatusCode)
	}

	return nil
}

// getMasterServerBaseURL returns the master server base URL from config.
// If the config value doesn't include a scheme (http/https), it adds one.
func (c *MasterServerConnector) getMasterServerBaseURL() string {
	honData := c.cfg.GetHoNData()
	url := honData.MasterServerURL
	if url == "" {
		return defaultMasterServerURL
	}
	// Ensure the URL has a scheme
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		// If it looks like an IP:port, use http://
		url = "http://" + url
	}
	// Remove trailing slash
	url = strings.TrimRight(url, "/")
	return url
}

func getPlatformString() string {
	if isWindows() {
		return "wac"
	}
	return "lac"
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
