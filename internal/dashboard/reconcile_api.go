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

	"github.com/merkle-works/x402-gateway/internal/pool"
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
	Pool        string `json:"pool"`
	Address     string `json:"address"`
	Checked     int    `json:"checked"`      // available UTXOs examined
	Valid       int    `json:"valid"`         // still unspent on-chain
	MarkedSpent int    `json:"marked_spent"`  // newly marked spent (zombie)
	Error       string `json:"error,omitempty"`
}

// handleReconcilePools checks all pool UTXOs against the blockchain (WoC)
// and marks any already-spent UTXOs as spent in the pool.
//
// This fixes "zombie" UTXOs that were spent on-chain by previous operations
// (e.g. test flows, delegator usage) but never marked spent in the pool
// due to the missing MarkSpent bug.
func (d *DashboardAPI) handleReconcilePools() http.HandlerFunc {
	logger := slog.Default().With("component", "dashboard.reconcile")

	return func(w http.ResponseWriter, r *http.Request) {
		if d.cfg.Broadcaster == "mock" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "reconciliation requires live mode (WoC broadcaster)",
			})
			return
		}

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
			result := reconcilePool(p.name, p.pool, baseURL, client, logger)
			results = append(results, result)
		}

		totalMarked := 0
		for _, r := range results {
			totalMarked += r.MarkedSpent
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success":       true,
			"pools":         results,
			"total_zombies": totalMarked,
		})
	}
}

// reconcilePool fetches on-chain unspent UTXOs for a pool's address from WoC,
// then marks any pool UTXOs not found on-chain as spent.
func reconcilePool(name string, p pool.Pool, baseURL string, client *http.Client, logger *slog.Logger) ReconcileResult {
	addr := p.Address()
	if addr == "" {
		return ReconcileResult{Pool: name, Error: "pool has no address"}
	}

	result := ReconcileResult{Pool: name, Address: addr}

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

	// Build lookup set: "txid:vout" → true
	onChainSet := make(map[string]bool, len(onChain))
	for _, u := range onChain {
		key := fmt.Sprintf("%s:%d", u.TxHash, u.TxPos)
		onChainSet[key] = true
	}

	// Compare pool UTXOs against on-chain set
	for _, u := range available {
		key := fmt.Sprintf("%s:%d", u.TxID, u.Vout)
		if onChainSet[key] {
			result.Valid++
		} else {
			// UTXO not found on-chain — it's been spent, mark it
			p.MarkSpent(u.TxID, u.Vout)
			result.MarkedSpent++
			logger.Info("reconcile: marked zombie spent",
				"pool", name,
				"outpoint", key,
				"satoshis", u.Satoshis,
			)
		}
	}

	logger.Info("reconcile complete",
		"pool", name,
		"address", addr,
		"checked", result.Checked,
		"valid", result.Valid,
		"marked_spent", result.MarkedSpent,
		"on_chain_utxos", len(onChain),
	)

	return result
}

// fetchUnspent queries WoC for all unspent UTXOs at an address.
func fetchUnspent(baseURL, address string, client *http.Client) ([]wocUnspent, error) {
	url := baseURL + "/address/" + address + "/unspent"

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("WoC request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// 404 = address with no history → empty
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("WoC rate limited (429) — try again in a few seconds")
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
