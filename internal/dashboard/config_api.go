package dashboard

import (
	"encoding/json"
	"net/http"
)

// ConfigResponse is the safe configuration response (no secret keys).
type ConfigResponse struct {
	Network                string  `json:"network"`
	Port                   int     `json:"port"`
	Broadcaster            string  `json:"broadcaster"`
	FeeRate                float64 `json:"feeRate"`
	PoolReplenishThreshold int     `json:"poolReplenishThreshold"`
	PoolOptimalSize        int     `json:"poolOptimalSize"`
	RedisEnabled           bool    `json:"redisEnabled"`
	PoolSize               int     `json:"poolSize"`
	LeaseTTL               int     `json:"leaseTTLSeconds"`
	PayeeAddress           string  `json:"payeeAddress"`
	KeyMode                string  `json:"keyMode"` // "xpriv" or "wif"
	FeeAddress             string  `json:"feeAddress"`
	PaymentAddress         string  `json:"paymentAddress"`
	TreasuryAddress        string  `json:"treasuryAddress"`
}

// ConfigUpdateRequest is the subset of config that can be changed at runtime.
type ConfigUpdateRequest struct {
	FeeRate                *float64 `json:"feeRate,omitempty"`
	PoolReplenishThreshold *int     `json:"poolReplenishThreshold,omitempty"`
	PoolOptimalSize        *int     `json:"poolOptimalSize,omitempty"`
}

// handleGetConfig returns the current (safe) configuration.
func (d *DashboardAPI) handleGetConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyMode := "wif"
		if d.cfg.XPRIV != "" {
			keyMode = "xpriv"
		}

		resp := ConfigResponse{
			Network:                d.cfg.BSVNetwork,
			Port:                   d.cfg.Port,
			Broadcaster:            d.cfg.Broadcaster,
			FeeRate:                d.cfg.FeeRate,
			PoolReplenishThreshold: d.cfg.PoolReplenishThreshold,
			PoolOptimalSize:        d.cfg.PoolOptimalSize,
			RedisEnabled:           d.cfg.RedisEnabled,
			PoolSize:               d.cfg.PoolSize,
			LeaseTTL:               int(d.cfg.LeaseTTL.Seconds()),
			PayeeAddress:           d.payeeAddr,
			KeyMode:                keyMode,
			FeeAddress:             d.keys.FeeAddress,
			PaymentAddress:         d.keys.PaymentAddress,
			TreasuryAddress:        d.keys.TreasuryAddress,
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// handleUpdateConfig applies runtime-adjustable configuration changes.
func (d *DashboardAPI) handleUpdateConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ConfigUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "invalid request body: " + err.Error(),
			})
			return
		}

		updated := make(map[string]any)

		if req.FeeRate != nil {
			if *req.FeeRate <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": "feeRate must be positive",
				})
				return
			}
			d.cfg.FeeRate = *req.FeeRate
			updated["feeRate"] = d.cfg.FeeRate
		}

		if req.PoolReplenishThreshold != nil {
			if *req.PoolReplenishThreshold < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": "poolReplenishThreshold must be non-negative",
				})
				return
			}
			d.cfg.PoolReplenishThreshold = *req.PoolReplenishThreshold
			updated["poolReplenishThreshold"] = d.cfg.PoolReplenishThreshold
		}

		if req.PoolOptimalSize != nil {
			if *req.PoolOptimalSize < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": "poolOptimalSize must be at least 1",
				})
				return
			}
			d.cfg.PoolOptimalSize = *req.PoolOptimalSize
			updated["poolOptimalSize"] = d.cfg.PoolOptimalSize
		}

		if len(updated) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "no fields to update",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"updated": updated,
		})
	}
}
