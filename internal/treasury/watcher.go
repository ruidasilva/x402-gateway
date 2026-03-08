// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//


package treasury

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"
	"github.com/redis/go-redis/v9"
)

// Compile-time check that TreasuryWatcher implements FundingSource.
var _ FundingSource = (*TreasuryWatcher)(nil)

const (
	redisKeyUTXOs    = "treasury:utxos"    // HASH: field="txid:vout", value=JSON
	redisKeyLastPoll = "treasury:lastPoll" // STRING: ISO 8601
)

// wocUnspent is the JSON response item from WoC's unspent endpoint.
type wocUnspent struct {
	Height int    `json:"height"`
	TxPos  int    `json:"tx_pos"`
	TxHash string `json:"tx_hash"`
	Value  int64  `json:"value"`
}

// TreasuryWatcher polls WhatsOnChain for unspent UTXOs at the treasury address
// and stores them in Redis (persistent) with an in-memory read cache.
// Falls back to in-memory only when Redis is not available.
type TreasuryWatcher struct {
	mu       sync.RWMutex
	utxos    []FundingUTXO // in-memory read cache
	lastPoll time.Time
	lastErr  error

	httpClient *http.Client
	baseURL    string // e.g. "https://api.whatsonchain.com/v1/bsv/main"
	address    string // treasury P2PKH address
	scriptHex  string // hex locking script (derived once)
	interval   time.Duration
	rdb        *redis.Client // nil if Redis not enabled
	logger     *slog.Logger
}

// NewTreasuryWatcher creates a watcher for the given treasury address.
// rdb may be nil (falls back to in-memory only).
func NewTreasuryWatcher(
	mainnet bool,
	address string,
	treasuryKey *ec.PrivateKey,
	interval time.Duration,
	rdb *redis.Client,
) (*TreasuryWatcher, error) {
	// Derive locking script hex from the treasury key
	addr, err := script.NewAddressFromPublicKey(treasuryKey.PubKey(), mainnet)
	if err != nil {
		return nil, fmt.Errorf("derive address: %w", err)
	}
	lockScript, err := p2pkh.Lock(addr)
	if err != nil {
		return nil, fmt.Errorf("derive locking script: %w", err)
	}
	scriptHex := fmt.Sprintf("%x", *lockScript)

	network := "test"
	if mainnet {
		network = "main"
	}

	return &TreasuryWatcher{
		utxos:    []FundingUTXO{},
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    fmt.Sprintf("https://api.whatsonchain.com/v1/bsv/%s", network),
		address:    address,
		scriptHex:  scriptHex,
		interval:   interval,
		rdb:        rdb,
		logger:     slog.Default().With("component", "treasury-watcher"),
	}, nil
}

// Start hydrates from Redis (if available), polls WoC immediately,
// then polls every interval. Stops when stop is closed.
func (tw *TreasuryWatcher) Start(stop <-chan struct{}) {
	tw.logger.Info("starting treasury watcher",
		"address", tw.address,
		"interval", tw.interval,
	)

	// Hydrate from Redis on startup
	if tw.rdb != nil {
		if err := tw.hydrateFromRedis(); err != nil {
			tw.logger.Warn("failed to hydrate from Redis", "error", err)
		} else {
			tw.mu.RLock()
			count := len(tw.utxos)
			tw.mu.RUnlock()
			if count > 0 {
				tw.logger.Info("hydrated from Redis", "utxo_count", count)
			}
		}
	}

	// Poll immediately
	if err := tw.poll(); err != nil {
		tw.logger.Warn("initial poll failed", "error", err)
	}

	go func() {
		ticker := time.NewTicker(tw.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := tw.poll(); err != nil {
					tw.logger.Error("poll failed", "error", err)
				}
			case <-stop:
				tw.logger.Info("treasury watcher stopped")
				return
			}
		}
	}()
}

// poll queries WoC for unspent UTXOs, updates Redis and in-memory cache.
func (tw *TreasuryWatcher) poll() error {
	url := tw.baseURL + "/address/" + tw.address + "/unspent"

	resp, err := tw.httpClient.Get(url)
	if err != nil {
		tw.mu.Lock()
		tw.lastErr = fmt.Errorf("WoC request failed: %w", err)
		tw.mu.Unlock()
		return tw.lastErr
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tw.mu.Lock()
		tw.lastErr = fmt.Errorf("read response body: %w", err)
		tw.mu.Unlock()
		return tw.lastErr
	}

	// WoC returns 404 for addresses with no history, treat as empty
	if resp.StatusCode == http.StatusNotFound {
		tw.mu.Lock()
		tw.utxos = []FundingUTXO{}
		tw.lastPoll = time.Now()
		tw.lastErr = nil
		tw.mu.Unlock()
		tw.persistToRedis([]FundingUTXO{})
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		tw.mu.Lock()
		tw.lastErr = fmt.Errorf("WoC returned HTTP %d: %s", resp.StatusCode, string(body))
		tw.mu.Unlock()
		return tw.lastErr
	}

	var items []wocUnspent
	if err := json.Unmarshal(body, &items); err != nil {
		tw.mu.Lock()
		tw.lastErr = fmt.Errorf("parse WoC response: %w", err)
		tw.mu.Unlock()
		return tw.lastErr
	}

	// Convert to FundingUTXO slice
	utxos := make([]FundingUTXO, 0, len(items))
	for _, item := range items {
		if item.Value <= 0 {
			continue
		}
		utxos = append(utxos, FundingUTXO{
			TxID:     item.TxHash,
			Vout:     uint32(item.TxPos),
			Script:   tw.scriptHex,
			Satoshis: uint64(item.Value),
		})
	}

	// Sort by value descending (largest first for GetFunding efficiency)
	sort.Slice(utxos, func(i, j int) bool {
		return utxos[i].Satoshis > utxos[j].Satoshis
	})

	// Update in-memory cache
	tw.mu.Lock()
	tw.utxos = utxos
	tw.lastPoll = time.Now()
	tw.lastErr = nil
	tw.mu.Unlock()

	// Persist to Redis
	tw.persistToRedis(utxos)

	if len(utxos) > 0 {
		tw.logger.Info("poll complete", "utxo_count", len(utxos))
	}

	return nil
}

// persistToRedis writes UTXOs to Redis atomically (delete old + set new).
func (tw *TreasuryWatcher) persistToRedis(utxos []FundingUTXO) {
	if tw.rdb == nil {
		return
	}

	ctx := context.Background()
	pipe := tw.rdb.Pipeline()

	// Delete the old hash entirely
	pipe.Del(ctx, redisKeyUTXOs)

	// Write each UTXO as a hash field
	for _, u := range utxos {
		field := fmt.Sprintf("%s:%d", u.TxID, u.Vout)
		data, err := json.Marshal(u)
		if err != nil {
			tw.logger.Error("marshal UTXO for Redis", "error", err)
			continue
		}
		pipe.HSet(ctx, redisKeyUTXOs, field, string(data))
	}

	// Update last poll timestamp
	pipe.Set(ctx, redisKeyLastPoll, time.Now().Format(time.RFC3339), 0)

	if _, err := pipe.Exec(ctx); err != nil {
		tw.logger.Error("persist to Redis", "error", err)
	}
}

// hydrateFromRedis loads UTXOs from Redis into the in-memory cache.
func (tw *TreasuryWatcher) hydrateFromRedis() error {
	if tw.rdb == nil {
		return nil
	}

	ctx := context.Background()

	// Read all fields from the hash
	result, err := tw.rdb.HGetAll(ctx, redisKeyUTXOs).Result()
	if err != nil {
		return fmt.Errorf("HGETALL %s: %w", redisKeyUTXOs, err)
	}

	utxos := make([]FundingUTXO, 0, len(result))
	for _, val := range result {
		var u FundingUTXO
		if err := json.Unmarshal([]byte(val), &u); err != nil {
			tw.logger.Warn("skip malformed Redis entry", "error", err)
			continue
		}
		utxos = append(utxos, u)
	}

	// Sort by value descending
	sort.Slice(utxos, func(i, j int) bool {
		return utxos[i].Satoshis > utxos[j].Satoshis
	})

	// Read last poll timestamp
	lastPollStr, err := tw.rdb.Get(ctx, redisKeyLastPoll).Result()
	if err == nil {
		if t, err := time.Parse(time.RFC3339, lastPollStr); err == nil {
			tw.mu.Lock()
			tw.lastPoll = t
			tw.mu.Unlock()
		}
	}

	tw.mu.Lock()
	tw.utxos = utxos
	tw.mu.Unlock()

	return nil
}

// GetUTXOs returns a copy of the current unspent UTXOs (from in-memory cache).
func (tw *TreasuryWatcher) GetUTXOs() []FundingUTXO {
	tw.mu.RLock()
	defer tw.mu.RUnlock()

	out := make([]FundingUTXO, len(tw.utxos))
	copy(out, tw.utxos)
	return out
}

// GetFunding returns the first UTXO with at least minSats value.
// Implements the FundingSource interface (defined in refill.go).
// Returns nil, nil if no qualifying UTXO is available.
func (tw *TreasuryWatcher) GetFunding(minSats uint64) (*FundingUTXO, error) {
	tw.mu.RLock()
	defer tw.mu.RUnlock()

	for i := range tw.utxos {
		if tw.utxos[i].Satoshis >= minSats {
			u := tw.utxos[i] // copy
			return &u, nil
		}
	}
	return nil, nil
}

// LastPoll returns the time of the last successful poll and any error from the last attempt.
func (tw *TreasuryWatcher) LastPoll() (time.Time, error) {
	tw.mu.RLock()
	defer tw.mu.RUnlock()
	return tw.lastPoll, tw.lastErr
}
