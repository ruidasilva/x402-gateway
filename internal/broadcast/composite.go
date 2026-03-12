// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0


package broadcast

import (
	"context"
	"log/slog"
	"strings"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// CompositeBroadcaster chains a primary broadcaster (GorillaPool ARC) with
// a fallback (WhatsOnChain). It attempts the primary first; on transport-level
// failures it falls back to the secondary. Application-level rejections (bad tx,
// double-spend, fee too low) are NOT retried on fallback — the same rejection
// would occur.
//
// CompositeBroadcaster also implements MempoolChecker. For status checks,
// if the primary returns "not found/not tracked" (which ARC does for txs it
// hasn't processed), the fallback is consulted as a supplement.
type CompositeBroadcaster struct {
	primary  transaction.Broadcaster
	fallback transaction.Broadcaster
	health   *HealthTracker
	logger   *slog.Logger
}

// NewCompositeBroadcaster creates a composite broadcaster.
// primary is typically GorillaPool ARC, fallback is typically WoC.
// health may be nil (health tracking disabled).
func NewCompositeBroadcaster(
	primary transaction.Broadcaster,
	fallback transaction.Broadcaster,
	health *HealthTracker,
) *CompositeBroadcaster {
	return &CompositeBroadcaster{
		primary:  primary,
		fallback: fallback,
		health:   health,
		logger:   slog.Default().With("component", "composite-broadcaster"),
	}
}

// Broadcast tries the primary broadcaster first. On transport-level failure,
// falls back to the secondary. Application-level failures are returned as-is.
func (c *CompositeBroadcaster) Broadcast(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	// Try primary
	success, failure := c.primary.Broadcast(tx)
	if failure == nil {
		// Primary succeeded
		c.recordHealth("gorilla", "broadcast", true, "")
		c.logger.Debug("broadcast via primary (GorillaPool)", "txid", success.Txid)
		return success, nil
	}

	// Primary failed — classify the error
	if !isTransportError(failure) {
		// Application error (bad tx, double-spend, etc.) — do NOT fallback
		c.recordHealth("gorilla", "broadcast", true, "") // service reachable, tx rejected
		c.logger.Info("primary rejected tx (application error, no fallback)",
			"code", failure.Code,
			"desc", failure.Description,
		)
		return nil, failure
	}

	// Transport error — try fallback
	c.recordHealth("gorilla", "broadcast", false, failure.Code+": "+failure.Description)
	c.logger.Warn("primary transport failure, falling back to WoC",
		"code", failure.Code,
		"desc", failure.Description,
	)

	success, failure = c.fallback.Broadcast(tx)
	if failure == nil {
		c.recordHealth("woc", "broadcast", true, "")
		c.logger.Info("broadcast via fallback (WoC)", "txid", success.Txid)
		return success, nil
	}

	c.recordHealth("woc", "broadcast", false, failure.Code+": "+failure.Description)
	c.logger.Error("fallback also failed",
		"code", failure.Code,
		"desc", failure.Description,
	)
	return nil, failure
}

// BroadcastCtx is the context-aware version. Same primary→fallback logic.
func (c *CompositeBroadcaster) BroadcastCtx(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	// Try primary (context-aware if supported)
	success, failure := broadcastWithCtx(c.primary, ctx, tx)
	if failure == nil {
		c.recordHealth("gorilla", "broadcast", true, "")
		return success, nil
	}

	if !isTransportError(failure) {
		c.recordHealth("gorilla", "broadcast", true, "")
		return nil, failure
	}

	c.recordHealth("gorilla", "broadcast", false, failure.Code+": "+failure.Description)
	c.logger.Warn("primary transport failure (ctx), falling back to WoC",
		"code", failure.Code,
	)

	success, failure = broadcastWithCtx(c.fallback, ctx, tx)
	if failure == nil {
		c.recordHealth("woc", "broadcast", true, "")
		return success, nil
	}

	c.recordHealth("woc", "broadcast", false, failure.Code+": "+failure.Description)
	return nil, failure
}

// broadcastWithCtx tries BroadcastCtx if the broadcaster supports it,
// otherwise falls back to Broadcast.
func broadcastWithCtx(b transaction.Broadcaster, ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	type ctxBroadcaster interface {
		BroadcastCtx(context.Context, *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure)
	}
	if cb, ok := b.(ctxBroadcaster); ok {
		return cb.BroadcastCtx(ctx, tx)
	}
	return b.Broadcast(tx)
}

// CheckMempool implements MempoolChecker. Tries the primary (ARC) first.
// If primary returns "not found" (visible=false, doubleSpend=false, err=nil)
// — which ARC does for txs it hasn't processed — the fallback (WoC) is
// consulted as a supplement.
//
// If primary returns a definitive answer (visible=true, or doubleSpend=true,
// or err!=nil), that answer is returned immediately without fallback.
func (c *CompositeBroadcaster) CheckMempool(txid string) (bool, bool, error) {
	// Try primary mempool check
	if checker, ok := c.primary.(MempoolChecker); ok {
		visible, doubleSpend, err := checker.CheckMempool(txid)

		if err != nil {
			// Primary error — could be transport; try fallback
			c.recordHealth("gorilla", "status", false, err.Error())
			c.logger.Warn("primary status check error, trying fallback",
				"txid", txid, "error", err,
			)
		} else if visible || doubleSpend {
			// Primary gave a definitive answer
			c.recordHealth("gorilla", "status", true, "")
			return visible, doubleSpend, nil
		} else {
			// Primary says "not found" — might just mean ARC doesn't track it.
			// Try fallback for supplemental visibility.
			c.recordHealth("gorilla", "status", true, "")
		}
	}

	// Fallback mempool check
	if checker, ok := c.fallback.(MempoolChecker); ok {
		visible, doubleSpend, err := checker.CheckMempool(txid)
		if err != nil {
			c.recordHealth("woc", "status", false, err.Error())
			return false, false, err
		}
		c.recordHealth("woc", "status", true, "")
		return visible, doubleSpend, nil
	}

	// Neither supports MempoolChecker — assume visible (safe fallback)
	return true, false, nil
}

// Health returns the health tracker (may be nil).
func (c *CompositeBroadcaster) Health() *HealthTracker {
	return c.health
}

// recordHealth records a success or failure in the health tracker.
func (c *CompositeBroadcaster) recordHealth(service, role string, success bool, errMsg string) {
	if c.health == nil {
		return
	}
	if success {
		c.health.RecordSuccess(service, role)
	} else {
		c.health.RecordFailure(service, role, errMsg)
	}
}

// ---------------------------------------------------------------------------
// isTransportError — error classification for fallback decisions
// ---------------------------------------------------------------------------

// isTransportError returns true if the broadcast failure indicates a
// transport/availability problem (network error, timeout, 5xx, rate limit)
// where retrying on a different service makes sense.
//
// Returns false for application-level errors (bad tx, double-spend, fee too low,
// script validation failed) where the same rejection would occur on any miner.
func isTransportError(f *transaction.BroadcastFailure) bool {
	if f == nil {
		return false
	}

	code := f.Code
	desc := strings.ToLower(f.Description)

	// Explicit transport error codes
	switch code {
	case "NETWORK_ERROR", "TIMEOUT":
		return true
	}

	// HTTP 5xx server errors
	if strings.HasPrefix(code, "5") || strings.HasPrefix(code, "HTTP_5") {
		return true
	}

	// HTTP 429 rate limited
	if code == "429" || code == "HTTP_429" {
		return true
	}

	// Description-based heuristics for network-level failures
	transportPatterns := []string{
		"connection refused",
		"no such host",
		"dial tcp",
		"tls handshake",
		"certificate",
		"context deadline",
		"context canceled",
		"i/o timeout",
		"eof",
	}
	for _, pattern := range transportPatterns {
		if strings.Contains(desc, pattern) {
			return true
		}
	}

	// Everything else is application-level (400, 409, 461, 463, REJECTED, etc.)
	return false
}
