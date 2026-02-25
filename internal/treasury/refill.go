package treasury

import (
	"log/slog"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/merkle-works/x402-gateway/internal/pool"
)

// RefillConfig configures the auto-refill loop for a single pool.
type RefillConfig struct {
	Pool               pool.Pool              // the pool to monitor and refill
	PoolName           string                 // descriptive name for logging ("nonce" or "fee")
	ReplenishThreshold int                    // trigger refill when available < this
	OptimalPoolSize    int                    // target size after refill
	CheckInterval      time.Duration          // how often to check pool level
	FeeRate            float64                // for the fan-out tx's own miner fee
	Key                *ec.PrivateKey         // signing key for fan-out tx
	Mainnet            bool                   // network flag
	Broadcaster        transaction.Broadcaster // for broadcasting the fan-out tx
	FundingSource      FundingSource          // provides funding UTXOs for fan-out
}

// FundingSource provides funding UTXOs for fan-out transactions.
// Implementations may scan a treasury address via WoC API or use a
// designated funding pool.
type FundingSource interface {
	// GetFunding returns a funding UTXO with at least minSats value.
	// Returns nil, nil if no funding is available (not an error, just means skip refill).
	GetFunding(minSats uint64) (*FundingUTXO, error)
}

// FundingUTXO represents a funding UTXO for fan-out transactions.
type FundingUTXO struct {
	TxID     string
	Vout     uint32
	Script   string // hex locking script
	Satoshis uint64
}

// StartRefillLoop starts a background goroutine that monitors a pool and
// triggers fan-out when the available count drops below the threshold.
//
// The loop checks every cfg.CheckInterval:
//  1. If available < ReplenishThreshold, calculate how many UTXOs are needed
//  2. Request a funding UTXO from the FundingSource
//  3. Build and broadcast a fan-out transaction
//  4. Add the new UTXOs to the pool via AddExisting()
func StartRefillLoop(cfg RefillConfig, stop <-chan struct{}) {
	logger := slog.Default().With(
		"component", "treasury-refill",
		"pool", cfg.PoolName,
	)

	logger.Info("starting auto-refill loop",
		"threshold", cfg.ReplenishThreshold,
		"target", cfg.OptimalPoolSize,
		"interval", cfg.CheckInterval,
	)

	go func() {
		ticker := time.NewTicker(cfg.CheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				doRefillCheck(cfg, logger)
			case <-stop:
				logger.Info("refill loop stopped")
				return
			}
		}
	}()
}

// doRefillCheck performs a single refill check and triggers fan-out if needed.
func doRefillCheck(cfg RefillConfig, logger *slog.Logger) {
	available := cfg.Pool.Available()

	if available >= cfg.ReplenishThreshold {
		return // pool is healthy, no action needed
	}

	needed := cfg.OptimalPoolSize - available
	if needed <= 0 {
		return
	}

	logger.Info("pool below threshold, initiating fan-out",
		"available", available,
		"threshold", cfg.ReplenishThreshold,
		"needed", needed,
	)

	if cfg.FundingSource == nil {
		logger.Warn("no funding source configured, skipping refill")
		return
	}

	// Calculate minimum funding needed: 1 sat per UTXO + estimated fee
	// Fee estimate: ~34 bytes per output + 148 bytes input + 10 bytes overhead
	estimatedSize := 10 + 148 + (needed * 34)
	estimatedFee := ceilSats(float64(estimatedSize) * cfg.FeeRate)
	if estimatedFee < 1 {
		estimatedFee = 1
	}
	minFunding := uint64(needed) + estimatedFee

	funding, err := cfg.FundingSource.GetFunding(minFunding)
	if err != nil {
		logger.Error("failed to get funding", "error", err)
		return
	}
	if funding == nil {
		logger.Warn("no funding available for refill",
			"min_sats_needed", minFunding,
		)
		return
	}

	// Build and broadcast the fan-out
	result, err := BuildFanout(cfg.Key, cfg.Mainnet, FanoutRequest{
		FundingTxID:     funding.TxID,
		FundingVout:     funding.Vout,
		FundingScript:   funding.Script,
		FundingSatoshis: funding.Satoshis,
		OutputCount:     needed,
		FeeRate:         cfg.FeeRate,
	}, cfg.Broadcaster)
	if err != nil {
		logger.Error("fan-out failed", "error", err)
		return
	}

	// Add the new UTXOs to the pool
	cfg.Pool.AddExisting(result.UTXOs)

	logger.Info("pool replenished",
		"txid", result.TxID,
		"utxos_added", len(result.UTXOs),
		"new_available", cfg.Pool.Available(),
	)
}
