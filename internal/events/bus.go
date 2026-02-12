package events

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

// HandlerFunc is a function that handles an event.
type HandlerFunc func(ctx context.Context, event Event) error

// EventBus implements an asynchronous publish-subscribe event system.
// It is the central communication backbone of Energizer, replacing the
// original Python EventBus with Go channels and goroutines.
type EventBus struct {
	mu          sync.RWMutex
	handlers    map[EventType][]handlerEntry
	stopCh      chan struct{}
	stopped     bool
	wg          sync.WaitGroup
}

type handlerEntry struct {
	name    string
	handler HandlerFunc
}

// NewEventBus creates a new EventBus instance.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[EventType][]handlerEntry),
		stopCh:   make(chan struct{}),
	}
}

// Subscribe registers a handler function for a specific event type.
// The name parameter is used for logging/debugging purposes.
func (eb *EventBus) Subscribe(eventType EventType, name string, handler HandlerFunc) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.handlers[eventType] = append(eb.handlers[eventType], handlerEntry{
		name:    name,
		handler: handler,
	})

	log.Debug().
		Str("event", string(eventType)).
		Str("handler", name).
		Msg("subscribed to event")
}

// Unsubscribe removes a named handler from a specific event type.
func (eb *EventBus) Unsubscribe(eventType EventType, name string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	handlers, exists := eb.handlers[eventType]
	if !exists {
		return
	}

	filtered := make([]handlerEntry, 0, len(handlers))
	for _, h := range handlers {
		if h.name != name {
			filtered = append(filtered, h)
		}
	}
	eb.handlers[eventType] = filtered

	log.Debug().
		Str("event", string(eventType)).
		Str("handler", name).
		Msg("unsubscribed from event")
}

// Emit publishes an event to all subscribed handlers asynchronously.
// Each handler runs in its own goroutine to prevent blocking.
func (eb *EventBus) Emit(ctx context.Context, event Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.stopped {
		return
	}

	handlers, exists := eb.handlers[event.Type]
	if !exists || len(handlers) == 0 {
		return
	}

	log.Trace().
		Str("event", string(event.Type)).
		Str("source", event.Source).
		Int("handlers", len(handlers)).
		Msg("emitting event")

	for _, h := range handlers {
		h := h // capture loop variable
		eb.wg.Add(1)
		go func() {
			defer eb.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Error().
						Str("event", string(event.Type)).
						Str("handler", h.name).
						Interface("panic", r).
						Msg("handler panicked")
				}
			}()

			if err := h.handler(ctx, event); err != nil {
				log.Error().
					Err(err).
					Str("event", string(event.Type)).
					Str("handler", h.name).
					Msg("handler returned error")
			}
		}()
	}
}

// EmitSync publishes an event and waits for all handlers to complete.
// Returns the first error encountered, if any.
func (eb *EventBus) EmitSync(ctx context.Context, event Event) error {
	eb.mu.RLock()
	if eb.stopped {
		eb.mu.RUnlock()
		return nil
	}

	handlers, exists := eb.handlers[event.Type]
	if !exists || len(handlers) == 0 {
		eb.mu.RUnlock()
		return nil
	}

	// Copy handlers to release lock before executing
	handlersCopy := make([]handlerEntry, len(handlers))
	copy(handlersCopy, handlers)
	eb.mu.RUnlock()

	var firstErr error
	var errOnce sync.Once
	var wg sync.WaitGroup

	for _, h := range handlersCopy {
		h := h
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Error().
						Str("event", string(event.Type)).
						Str("handler", h.name).
						Interface("panic", r).
						Msg("handler panicked")
				}
			}()

			if err := h.handler(ctx, event); err != nil {
				errOnce.Do(func() { firstErr = err })
				log.Error().
					Err(err).
					Str("event", string(event.Type)).
					Str("handler", h.name).
					Msg("handler returned error")
			}
		}()
	}

	wg.Wait()
	return firstErr
}

// Stop signals the EventBus to stop accepting new events and waits
// for all in-flight handlers to complete.
func (eb *EventBus) Stop() {
	eb.mu.Lock()
	eb.stopped = true
	close(eb.stopCh)
	eb.mu.Unlock()

	eb.wg.Wait()
	log.Info().Msg("event bus stopped")
}

// StopCh returns a channel that is closed when the EventBus is stopped.
func (eb *EventBus) StopCh() <-chan struct{} {
	return eb.stopCh
}

// HandlerCount returns the number of handlers registered for a specific event type.
func (eb *EventBus) HandlerCount(eventType EventType) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.handlers[eventType])
}
