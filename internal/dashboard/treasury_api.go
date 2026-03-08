package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/merkle-works/x402-gateway/internal/pool"
	"github.com/merkle-works/x402-gateway/internal/treasury"
)

// TreasuryInfoResponse provides treasury address and derivation details.
type TreasuryInfoResponse struct {
	Address        string `json:"address"`
	Network        string `json:"network"`
	Broadcaster    string `json:"broadcaster"`
	KeyMode        string `json:"keyMode"` // "xpriv" or "wif"
	DerivationPath string `json:"derivationPath,omitempty"`
	NoncePool      any    `json:"noncePool"`
	FeePool        any    `json:"feePool"`
	PaymentPool    any    `json:"paymentPool"`
}

// FanoutRequest is the request body for POST /api/v1/treasury/fanout.
type FanoutRequest struct {
	Pool            string `json:"pool"`            // "nonce", "fee", or "payment"
	Count           int    `json:"count"`           // number of UTXOs to create
	FundingTxID     string `json:"fundingTxid"`     // txid of funding UTXO
	FundingVout     uint32 `json:"fundingVout"`     // vout of funding UTXO
	FundingScript   string `json:"fundingScript"`   // locking script hex of funding UTXO
	FundingSatoshis uint64 `json:"fundingSatoshis"` // value of funding UTXO
	SigningKey      string `json:"signingKey"`      // optional: "treasury", "fee", or "payment" (default: treasury)
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

// TreasuryUTXOResponse is the response from GET /api/v1/treasury/utxos.
type TreasuryUTXOResponse struct {
	UTXOs    []TreasuryUTXO `json:"utxos"`
	LastPoll string         `json:"lastPoll,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// TreasuryUTXO represents an unspent UTXO at the treasury address.
type TreasuryUTXO struct {
	TxID     string `json:"txid"`
	Vout     uint32 `json:"vout"`
	Script   string `json:"script"`
	Satoshis uint64 `json:"satoshis"`
}

// handleTreasuryUTXOs returns unspent UTXOs at the treasury address.
func (d *DashboardAPI) handleTreasuryUTXOs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.watcher == nil {
			writeJSON(w, http.StatusOK, TreasuryUTXOResponse{
				UTXOs: []TreasuryUTXO{},
				Error: "treasury watcher not configured",
			})
			return
		}

		utxos := d.watcher.GetUTXOs()
		lastPoll, lastErr := d.watcher.LastPoll()

		resp := TreasuryUTXOResponse{
			UTXOs: make([]TreasuryUTXO, len(utxos)),
		}
		if !lastPoll.IsZero() {
			resp.LastPoll = lastPoll.Format(time.RFC3339)
		}
		if lastErr != nil {
			resp.Error = lastErr.Error()
		}
		for i, u := range utxos {
			resp.UTXOs[i] = TreasuryUTXO{
				TxID:     u.TxID,
				Vout:     u.Vout,
				Script:   u.Script,
				Satoshis: u.Satoshis,
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
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
			Broadcaster:    d.broadcaster.Mode(),
			KeyMode:        keyMode,
			DerivationPath: derivationPath,
			NoncePool:      d.noncePool.Stats(),
			FeePool:        d.feePool.Stats(),
			PaymentPool:    d.paymentPool.Stats(),
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
		if req.Pool != "nonce" && req.Pool != "fee" && req.Pool != "payment" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "pool must be 'nonce', 'fee', or 'payment'",
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

		// Determine signing key (default: treasury)
		signingKey := d.keys.TreasuryKey
		switch req.SigningKey {
		case "nonce":
			signingKey = d.keys.NonceKey
		case "fee":
			signingKey = d.keys.FeeKey
		case "payment":
			signingKey = d.keys.PaymentKey
		case "treasury", "":
			signingKey = d.keys.TreasuryKey
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "signingKey must be 'treasury', 'nonce', 'fee', or 'payment'",
			})
			return
		}

		// Determine target address and output denomination for each pool
		var targetAddr string
		var outputSats uint64 = 1 // default for nonce and fee pools
		switch req.Pool {
		case "nonce":
			targetAddr = d.keys.NonceAddress
		case "fee":
			targetAddr = d.keys.FeeAddress
		case "payment":
			targetAddr = d.keys.PaymentAddress
			outputSats = 100 // payment pool uses 100-sat UTXOs
		}

		// Build and broadcast fan-out transaction.
		// Change returns to the treasury address so it remains accessible
		// for future fan-outs (not stranded in the pool address).
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
				TargetAddress:   targetAddr,
				ChangeAddress:   d.keys.TreasuryAddress,
				OutputSatoshis:  outputSats,
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
		var targetPool pool.Pool
		switch req.Pool {
		case "nonce":
			targetPool = d.noncePool
		case "fee":
			targetPool = d.feePool
		case "payment":
			targetPool = d.paymentPool
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
