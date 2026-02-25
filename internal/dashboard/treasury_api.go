package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/merkle-works/x402-gateway/internal/treasury"
)

// TreasuryInfoResponse provides treasury address and derivation details.
type TreasuryInfoResponse struct {
	Address        string `json:"address"`
	Network        string `json:"network"`
	KeyMode        string `json:"keyMode"` // "xpriv" or "wif"
	DerivationPath string `json:"derivationPath,omitempty"`
	NoncePool      any    `json:"noncePool"`
	FeePool        any    `json:"feePool"`
}

// FanoutRequest is the request body for POST /api/v1/treasury/fanout.
type FanoutRequest struct {
	Pool            string `json:"pool"`            // "nonce" or "fee"
	Count           int    `json:"count"`           // number of 1-sat UTXOs to create
	FundingTxID     string `json:"fundingTxid"`     // txid of funding UTXO
	FundingVout     uint32 `json:"fundingVout"`     // vout of funding UTXO
	FundingScript   string `json:"fundingScript"`   // locking script hex of funding UTXO
	FundingSatoshis uint64 `json:"fundingSatoshis"` // value of funding UTXO
}

// FanoutResponse is the response from a successful fan-out operation.
type FanoutResponse struct {
	Success   bool   `json:"success"`
	TxID      string `json:"txid"`
	UTXOCount int    `json:"utxoCount"`
	Pool      string `json:"pool"`
}

// FanoutHistoryEntry records a past fan-out operation.
type FanoutHistoryEntry struct {
	TxID      string    `json:"txid"`
	Pool      string    `json:"pool"`
	Count     int       `json:"count"`
	Timestamp time.Time `json:"timestamp"`
}

// fanoutHistory is a thread-safe, bounded history of fan-out operations.
type fanoutHistory struct {
	mu      sync.Mutex
	entries []FanoutHistoryEntry
	maxSize int
}

var history = &fanoutHistory{maxSize: 100}

func (fh *fanoutHistory) add(entry FanoutHistoryEntry) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.entries = append(fh.entries, entry)
	if len(fh.entries) > fh.maxSize {
		fh.entries = fh.entries[len(fh.entries)-fh.maxSize:]
	}
}

func (fh *fanoutHistory) list() []FanoutHistoryEntry {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	out := make([]FanoutHistoryEntry, len(fh.entries))
	copy(out, fh.entries)
	return out
}

// handleTreasuryInfo returns treasury address and pool status.
func (d *DashboardAPI) handleTreasuryInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyMode := "wif"
		derivationPath := ""
		if d.cfg.XPRIV != "" {
			keyMode = "xpriv"
			derivationPath = "m/402'/0'/2'"
		}

		resp := TreasuryInfoResponse{
			Address:        d.keys.TreasuryAddress,
			Network:        d.cfg.BSVNetwork,
			KeyMode:        keyMode,
			DerivationPath: derivationPath,
			NoncePool:      d.noncePool.Stats(),
			FeePool:        d.feePool.Stats(),
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// handleTreasuryFanout triggers a manual fan-out operation.
func (d *DashboardAPI) handleTreasuryFanout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req FanoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "invalid request body: " + err.Error(),
			})
			return
		}

		// Validate pool selection
		if req.Pool != "nonce" && req.Pool != "fee" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "pool must be 'nonce' or 'fee'",
			})
			return
		}
		if req.Count < 1 || req.Count > 10000 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "count must be between 1 and 10000",
			})
			return
		}
		if req.FundingTxID == "" || req.FundingScript == "" || req.FundingSatoshis == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "fundingTxid, fundingScript, and fundingSatoshis are required",
			})
			return
		}

		// Determine which key to use for signing
		signingKey := d.keys.NonceKey
		if req.Pool == "fee" {
			signingKey = d.keys.FeeKey
		}

		// Build and broadcast fan-out transaction
		result, err := treasury.BuildFanout(
			signingKey,
			d.mainnet,
			treasury.FanoutRequest{
				FundingTxID:     req.FundingTxID,
				FundingVout:     req.FundingVout,
				FundingScript:   req.FundingScript,
				FundingSatoshis: req.FundingSatoshis,
				OutputCount:     req.Count,
				FeeRate:         d.cfg.FeeRate,
			},
			d.broadcaster,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": fmt.Sprintf("fan-out failed: %s", err),
			})
			return
		}

		// Add new UTXOs to the appropriate pool
		targetPool := d.noncePool
		if req.Pool == "fee" {
			targetPool = d.feePool
		}
		targetPool.AddExisting(result.UTXOs)

		// Record in history
		history.add(FanoutHistoryEntry{
			TxID:      result.TxID,
			Pool:      req.Pool,
			Count:     len(result.UTXOs),
			Timestamp: time.Now(),
		})

		writeJSON(w, http.StatusOK, FanoutResponse{
			Success:   true,
			TxID:      result.TxID,
			UTXOCount: len(result.UTXOs),
			Pool:      req.Pool,
		})
	}
}

// handleTreasuryHistory returns the list of past fan-out operations.
func (d *DashboardAPI) handleTreasuryHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries := history.list()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"history": entries,
		})
	}
}
