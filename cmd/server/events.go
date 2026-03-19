// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//


package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EventType classifies gateway events for the SSE stream.
type EventType string

const (
	EventChallengeIssued EventType = "challenge_issued"
	EventPaymentAccepted EventType = "payment_accepted"
	EventPaymentRejected EventType = "payment_rejected"
	EventHTTPRequest     EventType = "http_request"
)

// Event represents a single gateway event sent to dashboard subscribers.
type Event struct {
	Type      EventType         `json:"-"`
	Path      string            `json:"path"`
	Method    string            `json:"method"`
	Status    int               `json:"status"`
	DurationMS int64            `json:"duration_ms"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details,omitempty"`
}

// EventBus broadcasts events to all SSE subscribers using a fan-out pattern.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan Event]struct{}),
	}
}

// Subscribe registers a new SSE client and returns its channel.
func (eb *EventBus) Subscribe() chan Event {
	ch := make(chan Event, 32) // buffered to prevent blocking emitters
	eb.mu.Lock()
	eb.subscribers[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

// Unsubscribe removes a client's channel and closes it.
func (eb *EventBus) Unsubscribe(ch chan Event) {
	eb.mu.Lock()
	delete(eb.subscribers, ch)
	eb.mu.Unlock()
	close(ch)
}

// Emit broadcasts an event to all subscribers. Non-blocking — drops events for slow clients.
func (eb *EventBus) Emit(e Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for ch := range eb.subscribers {
		select {
		case ch <- e:
		default:
			// Drop event if subscriber buffer is full
		}
	}
}

// handleEvents returns an HTTP handler for the SSE /demo/events endpoint.
func handleEvents(eventBus *EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := eventBus.Subscribe()
		defer eventBus.Unsubscribe(ch)

		// Send initial ping
		fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
				flusher.Flush()
			}
		}
	}
}

// eventTypeFromStatus classifies an HTTP response into an event type
// based on the status code and request path.
func eventTypeFromStatus(status int, path string) EventType {
	switch {
	case status == 402:
		return EventChallengeIssued
	case status == 200 && strings.HasPrefix(path, "/v1/"):
		return EventPaymentAccepted
	case status >= 400 && strings.HasPrefix(path, "/v1/"):
		return EventPaymentRejected
	default:
		return EventHTTPRequest
	}
}

// eventDetailsFromHeaders extracts relevant headers for event display.
func eventDetailsFromHeaders(rw *responseWriter, r *http.Request) map[string]string {
	details := make(map[string]string)

	// Response headers (per spec: X402-Receipt, X402-Challenge)
	if receipt := rw.Header().Get("X402-Receipt"); receipt != "" {
		details["receipt"] = truncateStr(receipt, 16)
	}
	if ch := rw.Header().Get("X402-Challenge"); ch != "" {
		details["has_challenge"] = "true"
	}

	// Request headers (per spec: X402-Proof)
	if proof := r.Header.Get("X402-Proof"); proof != "" {
		details["has_proof"] = "true"
	}

	return details
}

// truncateStr shortens a string and appends "..." if it exceeds maxLen.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
