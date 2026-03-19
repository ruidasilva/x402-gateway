// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0


package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/merkleworks/x402-bsv/internal/pool"
)

// wocUnspent matches the WoC /address/{address}/unspent response item.
type wocUnspent struct {
	Height int    `json:"height"`
	TxPos  int    `json:"tx_pos"`
	TxHash string `json:"tx_hash"`
	Value  int64  `json:"value"`
}

// ReconcileResult is the per-pool reconciliation outcome.
type ReconcileResult struct {
	Pool            string `json:"pool"`
	Address         string `json:"address"`
	Checked         int    `json:"checked"`                    // available UTXOs examined
	Valid           int    `json:"valid"`                      // still unspent on-chain
	MarkedSpent     int    `json:"marked_spent"`               // newly marked spent (zombie)
	DryRun          bool   `json:"dry_run,omitempty"`          // true if no mutations were performed
	Aborted         bool   `json:"aborted,omitempty"`          // true if safety threshold prevented action
	OnChainCount    int    `json:"on_chain_count,omitempty"`   // UTXOs found on-chain for this address
	Error           string `json:"error,omitempty"`
}

// SafetyThresholdPct is the maximum percentage of available UTXOs that can be
// marked as zombies in a single reconciliation run. If more than this fraction
// would be marked spent, the operation aborts — it's far more likely the API
// returned bad data than that all UTXOs were genuinely spent.
const SafetyThresholdPct = 50

// handleReconcilePools checks all pool UTXOs against the blockchain (WoC)
// and marks any already-spent UTXOs as spent in the pool.
//
// This fixes "zombie" UTXOs that were spent on-chain by previous operations
// (e.g. test flows, delegator usage) but never marked spent in the pool
// due to the missing MarkSpent bug.
//
// Safety features:
//   - dry_run=true (query param): reports what would be marked but doesn't mutate
//   - force=true (query param): bypasses the 50% safety threshold (requires explicit opt-in)
//   - Empty on-chain response guard: refuses to mark everything as zombie
//   - Threshold check: aborts if >50% of pool UTXOs would be marked spent
func (d *DashboardAPI) handleReconcilePools() http.HandlerFunc {
	logger := slog.Default().With("component", "dashboard.reconcile")

	return func(w http.ResponseWriter, r *http.Request) {
		if d.cfg.Broadcaster == "mock" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "reconciliation requires live mode (WoC broadcaster)",
			})
			return
		}

		dryRun := r.URL.Query().Get("dry_run") == "true"
		force := r.URL.Query().Get("force") == "true"

		baseURL := d.wocBaseURL
		client := &http.Client{Timeout: 15 * time.Second}

		pools := []struct {
			name string
			pool pool.Pool
		}{
			{"nonce", d.noncePool},
			{"fee", d.feePool},
			{"payment", d.paymentPool},
		}

		results := make([]ReconcileResult, 0, len(pools))

		for _, p := range pools {
			result := reconcilePool(p.name, p.pool, baseURL, client, logger, dryRun, force)
			results = append(results, result)
		}

		totalMarked := 0
		for _, r := range results {
			totalMarked += r.MarkedSpent
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success":       true,
			"dry_run":       dryRun,
			"pools":         results,
			"total_zombies": totalMarked,
		})
	}
}

// reconcilePool fetches on-chain unspent UTXOs for a pool's address from WoC,
// then marks any pool UTXOs not found on-chain as spent.
//
// Safety invariants:
//  1. Empty on-chain response → abort (likely API failure, not genuine empty)
//  2. >50% would be marked → abort unless force=true (likely bad data)
//  3. dry_run=true → report only, no mutations
func reconcilePool(name string, p pool.Pool, baseURL string, client *http.Client, logger *slog.Logger, dryRun, force bool) ReconcileResult {
	addr := p.Address()
	if addr == "" {
		return ReconcileResult{Pool: name, Error: "pool has no address", DryRun: dryRun}
	}

	result := ReconcileResult{Pool: name, Address: addr, DryRun: dryRun}

	// Get available UTXOs from the pool
	available, err := p.ListAvailable()
	if err != nil {
		result.Error = fmt.Sprintf("ListAvailable failed: %s", err)
		logger.Error("reconcile: ListAvailable failed", "pool", name, "error", err)
		return result
	}

	result.Checked = len(available)
	if len(available) == 0 {
		logger.Info("reconcile: pool empty", "pool", name)
		return result
	}

	// Fetch unspent UTXOs from WoC
	onChain, err := fetchUnspent(baseURL, addr, client)
	if err != nil {
		result.Error = fmt.Sprintf("WoC fetch failed: %s", err)
		logger.Error("reconcile: WoC fetch failed", "pool", name, "address", addr, "error", err)
		return result
	}

	result.OnChainCount = len(onChain)

	// SAFETY INVARIANT 1: If on-chain returns empty but pool has UTXOs,
	// the API likely failed silently (404 from deprecated endpoint, rate limit, etc.).
	// Refuse to mark everything as zombie — that's almost certainly wrong.
	if len(onChain) == 0 && len(available) > 0 {
		result.Error = fmt.Sprintf("SAFETY ABORT: WoC returned 0 on-chain UTXOs but pool has %d available — refusing to mark all as zombies (likely API failure or deprecated endpoint)", len(available))
		result.Aborted = true
		logger.Error("reconcile: SAFETY ABORT — empty on-chain response",
			"pool", name,
			"address", addr,
			"pool_available", len(available),
		)
		return result
	}

	// Build lookup set: "txid:vout" → true
	onChainSet := make(map[string]bool, len(onChain))
	for _, u := range onChain {
		key := fmt.Sprintf("%s:%d", u.TxHash, u.TxPos)
		onChainSet[key] = true
	}

	// First pass: count zombies without mutating
	zombieOutpoints := make([]struct{ txid string; vout uint32; key string; sats uint64 }, 0)
	validCount := 0
	for _, u := range available {
		key := fmt.Sprintf("%s:%d", u.TxID, u.Vout)
		if onChainSet[key] {
			validCount++
		} else {
			zombieOutpoints = append(zombieOutpoints, struct{ txid string; vout uint32; key string; sats uint64 }{u.TxID, u.Vout, key, u.Satoshis})
		}
	}

	// SAFETY INVARIANT 2: If more than SafetyThresholdPct% would be marked,
	// abort unless force=true — probably bad API data, not genuine spend.
	zombiePct := (len(zombieOutpoints) * 100) / len(available)
	if !force && zombiePct > SafetyThresholdPct {
		result.Error = fmt.Sprintf("SAFETY ABORT: %d of %d UTXOs (%d%%) would be marked as zombies, exceeding %d%% threshold — use force=true to override",
			len(zombieOutpoints), len(available), zombiePct, SafetyThresholdPct)
		result.Aborted = true
		result.Valid = validCount
		result.MarkedSpent = 0 // nothing mutated
		logger.Error("reconcile: SAFETY ABORT — threshold exceeded",
			"pool", name,
			"address", addr,
			"zombies", len(zombieOutpoints),
			"available", len(available),
			"pct", zombiePct,
			"threshold", SafetyThresholdPct,
		)
		return result
	}

	result.Valid = validCount

	// Second pass: apply mutations (unless dry_run)
	for _, z := range zombieOutpoints {
		if !dryRun {
			p.MarkSpent(z.txid, z.vout)
		}
		result.MarkedSpent++
		logger.Info("reconcile: zombie detected",
			"pool", name,
			"outpoint", z.key,
			"satoshis", z.sats,
			"dry_run", dryRun,
		)
	}

	logger.Info("reconcile complete",
		"pool", name,
		"address", addr,
		"checked", result.Checked,
		"valid", result.Valid,
		"marked_spent", result.MarkedSpent,
		"on_chain_utxos", len(onChain),
		"dry_run", dryRun,
	)

	return result
}

// fetchUnspent queries WoC for all confirmed unspent UTXOs at an address.
//
// IMPORTANT: Uses /address/{addr}/confirmed/unspent (the current WoC endpoint).
// The old /address/{addr}/unspent endpoint was deprecated and returns 404,
// which previously caused catastrophic false-zombie classification.
//
// Error handling: all non-200 responses return an error (including 404).
// The caller must never receive an empty slice from a failed API call.
func fetchUnspent(baseURL, address string, client *http.Client) ([]wocUnspent, error) {
	url := baseURL + "/address/" + address + "/confirmed/unspent"

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("WoC request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("WoC rate limited (429) — try again in a few seconds")
	}

	// 404 is now an ERROR, not "empty set". The confirmed/unspent endpoint
	// returns 200 with an empty array for addresses with no unspent outputs.
	// A 404 means the endpoint itself doesn't exist (API change).
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("WoC returned 404 — endpoint may be deprecated or address format invalid: %s", url)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("WoC returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var items []wocUnspent
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("parse WoC response: %w", err)
	}

	return items, nil
}
