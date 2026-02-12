// Package telemetry handles MQTT telemetry publishing and Filebeat integration.
package telemetry

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"

	"github.com/energizer-project/energizer/internal/config"
	"github.com/energizer-project/energizer/internal/events"
	"github.com/energizer-project/energizer/internal/util"
)

// MQTT topic prefixes
const (
	TopicManagerAdmin   = "manager/admin"
	TopicManagerStatus  = "manager/status"
	TopicManagerCommand = "manager/command"
	TopicGameStatus     = "game_server/status"
	TopicGameMatch      = "game_server/match"
	TopicGameLag        = "game_server/lag"
)

// MQTTHandler manages the MQTT connection and publishes telemetry events.
// It replaces the Python MQTTHandler with TLS/mTLS support.
type MQTTHandler struct {
	mu sync.Mutex

	cfg      *config.Config
	eventBus *events.EventBus
	client   mqtt.Client

	// Metadata included in every message
	metadata map[string]interface{}
}

// NewMQTTHandler creates a new MQTT telemetry handler.
func NewMQTTHandler(cfg *config.Config, eventBus *events.EventBus) (*MQTTHandler, error) {
	mqttCfg := cfg.ApplicationData.MQTT

	if !mqttCfg.Enabled {
		return nil, fmt.Errorf("MQTT is disabled")
	}

	// Build system metadata
	sysInfo := util.GetSystemInfo()
	metadata := map[string]interface{}{
		"hostname":     sysInfo.Hostname,
		"platform":     sysInfo.Platform,
		"cpu_model":    sysInfo.CPUModel,
		"cpu_cores":    sysInfo.CPUCores,
		"memory_mb":    sysInfo.TotalMemory,
		"app_version":  "1.0.0",
	}

	handler := &MQTTHandler{
		cfg:      cfg,
		eventBus: eventBus,
		metadata: metadata,
	}

	// Configure MQTT client
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("ssl://%s:%d", mqttCfg.BrokerURL, mqttCfg.Port))

	if mqttCfg.ClientID != "" {
		opts.SetClientID(mqttCfg.ClientID)
	} else {
		opts.SetClientID(fmt.Sprintf("energizer-%s", sysInfo.Hostname))
	}

	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetCleanSession(false)

	// TLS configuration
	if mqttCfg.UseTLS {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		// mTLS: load client certificate
		if mqttCfg.CertFile != "" && mqttCfg.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(mqttCfg.CertFile, mqttCfg.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load MQTT TLS certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		opts.SetTLSConfig(tlsConfig)
	}

	// Connection callbacks
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Info().Msg("MQTT connected")
	})

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Warn().Err(err).Msg("MQTT connection lost")
	})

	handler.client = mqtt.NewClient(opts)

	return handler, nil
}

// Start connects to the MQTT broker and subscribes to events.
func (h *MQTTHandler) Start(ctx context.Context) error {
	log.Info().
		Str("broker", h.cfg.ApplicationData.MQTT.BrokerURL).
		Int("port", h.cfg.ApplicationData.MQTT.Port).
		Msg("connecting to MQTT broker")

	token := h.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT connect failed: %w", token.Error())
	}

	// Subscribe to EventBus events for publishing
	h.subscribeEvents()

	// Block until context cancelled
	<-ctx.Done()

	h.PublishShutdown()
	h.client.Disconnect(5000)
	log.Info().Msg("MQTT disconnected")

	return nil
}

// subscribeEvents registers event handlers for MQTT publishing.
func (h *MQTTHandler) subscribeEvents() {
	h.eventBus.Subscribe(events.EventServerStatus, "mqtt.serverStatus", h.onServerStatus)
	h.eventBus.Subscribe(events.EventLobbyCreated, "mqtt.lobbyCreated", h.onLobbyCreated)
	h.eventBus.Subscribe(events.EventLobbyClosed, "mqtt.lobbyClosed", h.onLobbyClosed)
	h.eventBus.Subscribe(events.EventPlayerConnection, "mqtt.playerConnection", h.onPlayerConnection)
	h.eventBus.Subscribe(events.EventLongFrame, "mqtt.longFrame", h.onLongFrame)
	h.eventBus.Subscribe(events.EventNotifyMQTT, "mqtt.notify", h.onNotify)
}

// publish sends a JSON message to an MQTT topic.
func (h *MQTTHandler) publish(topic string, payload interface{}) {
	if !h.client.IsConnected() {
		return
	}

	// Merge metadata with payload
	msg := h.buildMessage(payload)

	data, err := json.Marshal(msg)
	if err != nil {
		log.Warn().Err(err).Str("topic", topic).Msg("failed to marshal MQTT message")
		return
	}

	token := h.client.Publish(topic, 1, false, data) // QoS 1
	go func() {
		token.Wait()
		if token.Error() != nil {
			log.Warn().Err(token.Error()).Str("topic", topic).Msg("MQTT publish failed")
		}
	}()
}

// buildMessage combines metadata with the event payload.
func (h *MQTTHandler) buildMessage(payload interface{}) map[string]interface{} {
	msg := make(map[string]interface{})

	// Add metadata
	for k, v := range h.metadata {
		msg[k] = v
	}

	// Add payload
	msg["payload"] = payload
	msg["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	return msg
}

// Event handlers

func (h *MQTTHandler) onServerStatus(ctx context.Context, event events.Event) error {
	h.publish(TopicGameStatus, event.Payload)
	return nil
}

func (h *MQTTHandler) onLobbyCreated(ctx context.Context, event events.Event) error {
	h.publish(TopicGameMatch, map[string]interface{}{
		"event":   "lobby_created",
		"payload": event.Payload,
	})
	return nil
}

func (h *MQTTHandler) onLobbyClosed(ctx context.Context, event events.Event) error {
	h.publish(TopicGameMatch, map[string]interface{}{
		"event":   "lobby_closed",
		"payload": event.Payload,
	})
	return nil
}

func (h *MQTTHandler) onPlayerConnection(ctx context.Context, event events.Event) error {
	h.publish(TopicGameMatch, map[string]interface{}{
		"event":   "player_connection",
		"payload": event.Payload,
	})
	return nil
}

func (h *MQTTHandler) onLongFrame(ctx context.Context, event events.Event) error {
	h.publish(TopicGameLag, event.Payload)
	return nil
}

func (h *MQTTHandler) onNotify(ctx context.Context, event events.Event) error {
	h.publish(TopicManagerStatus, event.Payload)
	return nil
}

// PublishShutdown sends a shutdown message to the MQTT broker.
func (h *MQTTHandler) PublishShutdown() {
	h.publish(TopicManagerAdmin, map[string]interface{}{
		"event":     "shutdown",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
