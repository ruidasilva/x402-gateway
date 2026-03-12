// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0


package broadcast

import (
	"sync"
	"time"
)

// ServiceHealth represents the health status of a single broadcaster service.
type ServiceHealth struct {
	Healthy   bool      `json:"healthy"`
	LastCheck time.Time `json:"lastCheck"`
	LastError string    `json:"lastError,omitempty"`
	Service   string    `json:"service"` // "gorilla", "woc"
	Role      string    `json:"role"`    // "broadcast", "status"
}

// HealthTracker tracks the health of broadcaster services.
// It records successes and failures for each (service, role) pair,
// allowing the dashboard to display per-service health indicators.
type HealthTracker struct {
	mu       sync.RWMutex
	services map[string]*ServiceHealth // key = "gorilla:broadcast", "woc:status", etc.
}

// NewHealthTracker creates a new health tracker.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		services: make(map[string]*ServiceHealth),
	}
}

// key builds the map key for a (service, role) pair.
func key(service, role string) string {
	return service + ":" + role
}

// RecordSuccess marks a service+role as healthy.
func (h *HealthTracker) RecordSuccess(service, role string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	k := key(service, role)
	sh, ok := h.services[k]
	if !ok {
		sh = &ServiceHealth{Service: service, Role: role}
		h.services[k] = sh
	}
	sh.Healthy = true
	sh.LastCheck = time.Now()
	sh.LastError = ""
}

// RecordFailure marks a service+role as unhealthy with an error message.
func (h *HealthTracker) RecordFailure(service, role, errMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	k := key(service, role)
	sh, ok := h.services[k]
	if !ok {
		sh = &ServiceHealth{Service: service, Role: role}
		h.services[k] = sh
	}
	sh.Healthy = false
	sh.LastCheck = time.Now()
	sh.LastError = errMsg
}

// Get returns the health status for a specific service+role, or nil if not tracked yet.
func (h *HealthTracker) Get(service, role string) *ServiceHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if sh, ok := h.services[key(service, role)]; ok {
		// Return a copy to avoid data races on the caller side
		cp := *sh
		return &cp
	}
	return nil
}

// All returns a snapshot of all tracked service health statuses.
func (h *HealthTracker) All() map[string]*ServiceHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]*ServiceHealth, len(h.services))
	for k, v := range h.services {
		cp := *v
		result[k] = &cp
	}
	return result
}
