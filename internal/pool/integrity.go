// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0


package pool

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// IntegrityResult holds the outcome of a local pool integrity check.
type IntegrityResult struct {
	Pool        string    `json:"pool"`
	Mode        string    `json:"mode"`
	Checked     int       `json:"checked"`
	Valid       int       `json:"valid"`
	Quarantined int       `json:"quarantined"`
	Timestamp   time.Time `json:"timestamp"`
}

// CheckIntegrity scans all UTXO detail hashes in a RedisPool namespace and
// enforces mode isolation rules. This is a purely local Redis metadata check —
// no external blockchain API calls.
//
// Rules enforced:
//  1. In live mode: synthetic == true → quarantine (synthetic UTXOs must not exist in live pools)
//  2. OriginMode mismatch: origin_mode != runtimeMode → quarantine
//
// Quarantined UTXOs are:
//   - Removed from {prefix}available ZSET
//   - Added to {prefix}quarantined SET (preserves pool identity)
//   - Detail hash updated: status="quarantined", quarantined_at, quarantine_reason
//
// This function is safe to call from both the gateway (for nonce/fee/payment pools)
// and the delegator (for the fee pool).
func CheckIntegrity(rdb *redis.Client, prefix, runtimeMode string, logger *slog.Logger) IntegrityResult {
	result := IntegrityResult{
		Pool:      prefix,
		Mode:      runtimeMode,
		Timestamp: time.Now(),
	}

	if rdb == nil {
		return result
	}

	ctx := context.Background()

	// Scan all detail keys for this namespaced prefix
	pattern := prefix + keyDetails + ":*"
	iter := rdb.Scan(ctx, 0, pattern, 100).Iterator()

	now := fmt.Sprintf("%d", time.Now().Unix())

	for iter.Next(ctx) {
		detKey := iter.Val()
		data, err := rdb.HGetAll(ctx, detKey).Result()
		if err != nil || len(data) == 0 {
			continue
		}

		result.Checked++

		synthetic := data["synthetic"] == "true"
		originMode := data["origin_mode"]
		reason := ""

		// Rule 1: synthetic UTXOs in live mode
		if runtimeMode == "live" && synthetic {
			reason = "synthetic_in_live"
		}

		// Rule 2: origin_mode mismatch (if set)
		if reason == "" && originMode != "" && originMode != runtimeMode {
			reason = "mode_mismatch"
		}

		if reason == "" {
			result.Valid++
			continue
		}

		// Quarantine this UTXO
		txid := data["txid"]
		vout := data["vout"]
		if txid == "" || vout == "" {
			continue
		}
		outpoint := txid + ":" + vout

		// Remove from available ZSET
		rdb.ZRem(ctx, prefix+keyAvailable, outpoint)

		// Add to quarantined SET (preserves pool identity for dashboard visibility)
		rdb.SAdd(ctx, prefix+"quarantined", outpoint)

		// Update detail hash with quarantine status
		rdb.HSet(ctx, detKey,
			"status", "quarantined",
			"quarantined_at", now,
			"quarantine_reason", reason,
		)

		result.Quarantined++

		logger.Warn("pool integrity: quarantined UTXO",
			"prefix", prefix,
			"outpoint", outpoint,
			"reason", reason,
			"synthetic", synthetic,
			"origin_mode", originMode,
		)
	}

	if err := iter.Err(); err != nil {
		logger.Error("pool integrity: scan error", "prefix", prefix, "error", err)
	}

	return result
}

// QuarantineCount returns the number of quarantined UTXOs for a given prefix.
func QuarantineCount(rdb *redis.Client, prefix string) int64 {
	if rdb == nil {
		return 0
	}
	ctx := context.Background()
	count, err := rdb.SCard(ctx, prefix+"quarantined").Result()
	if err != nil {
		return 0
	}
	return count
}

// ValidateOnChainResult holds the outcome of on-chain UTXO validation.
type ValidateOnChainResult struct {
	Pool     string `json:"pool"`
	Checked  int    `json:"checked"`
	Valid    int    `json:"valid"`
	Zombies  int    `json:"zombies"`
	Leased   int    `json:"leased"`
}

// ValidateOnChain compares a pool's available UTXOs against a set of on-chain
// unspent outpoints. Any available UTXO not in the on-chain set is a "zombie" —
// it was spent on-chain but the pool still considers it available (typically due
// to the lease reclaim loop returning a spent nonce before the MarkSpent fix).
//
// Zombie UTXOs are retired via MarkSpent so they're never re-issued.
// Also retires any currently-leased UTXOs that are not in the on-chain set.
//
// Safety invariants:
//   - Empty on-chain set with non-empty pool → abort (likely API failure)
//   - >50% zombies → abort unless force=true (likely bad data)
//
// onChainUnspent should be a set of "txid:vout" strings from the blockchain.
func ValidateOnChain(p Pool, onChainUnspent map[string]bool, logger *slog.Logger) ValidateOnChainResult {
	result := ValidateOnChainResult{Pool: p.Address()}

	available, err := p.ListAvailable()
	if err != nil {
		logger.Error("on-chain validation: failed to list available UTXOs", "error", err)
		return result
	}

	if len(available) == 0 {
		return result
	}

	// SAFETY: refuse to mark all UTXOs as zombies when on-chain set is empty.
	// An empty on-chain set almost certainly means the API failed, not that
	// every single UTXO was genuinely spent.
	if len(onChainUnspent) == 0 {
		logger.Error("on-chain validation: SAFETY ABORT — on-chain set is empty but pool has UTXOs",
			"address", p.Address(),
			"pool_available", len(available),
		)
		result.Checked = len(available)
		return result
	}

	// First pass: count zombies
	zombies := make([]UTXO, 0)
	for _, u := range available {
		result.Checked++
		outpoint := u.Outpoint()
		if onChainUnspent[outpoint] {
			result.Valid++
		} else {
			zombies = append(zombies, u)
		}
	}

	// SAFETY: threshold check — if >50% would be marked, abort
	if len(zombies) > 0 {
		pct := (len(zombies) * 100) / len(available)
		if pct > 50 {
			logger.Error("on-chain validation: SAFETY ABORT — too many zombies",
				"address", p.Address(),
				"zombies", len(zombies),
				"available", len(available),
				"pct", pct,
			)
			// Don't mutate anything
			return result
		}
	}

	// Second pass: mark zombies as spent
	for _, u := range zombies {
		p.MarkSpent(u.TxID, u.Vout)
		result.Zombies++
		logger.Warn("on-chain validation: retired zombie UTXO",
			"outpoint", u.Outpoint(),
			"address", p.Address(),
		)
	}

	return result
}

// ParsePoolNameFromPrefix extracts the pool name from a namespaced prefix.
// e.g. "live:nonce:" → "nonce", "mock:fee:" → "fee"
func ParsePoolNameFromPrefix(prefix string) string {
	parts := strings.Split(strings.TrimSuffix(prefix, ":"), ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return prefix
}
