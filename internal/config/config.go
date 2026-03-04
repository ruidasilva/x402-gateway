package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all gateway configuration.
type Config struct {
	// BSV private key in WIF format (legacy single-key mode)
	BSVPrivateKey string

	// BIP32 extended private key (HD wallet mode, alternative to BSV_PRIVATE_KEY)
	XPRIV string

	// BSV network: "mainnet" or "testnet"
	BSVNetwork string

	// Service payee address
	PayeeAddress string

	// HTTP server port
	Port int

	// Pool initial size (fee + payment pools)
	PoolSize int

	// Pool lease TTL
	LeaseTTL time.Duration

	// Fee rate in sat/byte (BSV standard: 1 sat/KB = 0.001 sat/byte)
	FeeRate float64

	// Broadcaster type: "woc" or "arc"
	Broadcaster string

	// Daily fee budget in satoshis (0 = unlimited)
	DailyFeeBudget uint64

	// Redis configuration
	RedisURL     string // REDIS_URL (e.g. redis://localhost:6379)
	RedisEnabled bool   // REDIS_ENABLED (true = Redis pools, false = in-memory)

	// Pool thresholds for auto-refill
	PoolReplenishThreshold int // trigger refill when available < this (default 500)
	PoolOptimalSize        int // target size after refill (default 5000)

	// Treasury UTXO watcher poll interval in seconds (0 = disabled)
	TreasuryPollInterval int

	// Backward-compatible aliases (deprecated, use PoolSize/LeaseTTL)
	NoncePoolSize int
	NonceLeaseTTL time.Duration
}

// Load reads configuration from environment variables.
// Required: one of XPRIV or BSV_PRIVATE_KEY, plus FEE_RATE and BROADCASTER.
func Load() (*Config, error) {
	poolSize := envIntOrDefault("POOL_SIZE", envIntOrDefault("NONCE_POOL_SIZE", 100))
	leaseTTL := time.Duration(envIntOrDefault("LEASE_TTL", envIntOrDefault("NONCE_LEASE_TTL", 300))) * time.Second

	cfg := &Config{
		BSVPrivateKey:  os.Getenv("BSV_PRIVATE_KEY"),
		XPRIV:          os.Getenv("XPRIV"),
		BSVNetwork:     envOrDefault("BSV_NETWORK", "testnet"),
		PayeeAddress:   os.Getenv("PAYEE_ADDRESS"),
		Port:           envIntOrDefault("PORT", 8402),
		PoolSize:       poolSize,
		LeaseTTL:       leaseTTL,
		FeeRate:        envFloatOrDefault("FEE_RATE", 0),
		Broadcaster:    os.Getenv("BROADCASTER"),
		DailyFeeBudget:         uint64(envIntOrDefault("DAILY_FEE_BUDGET", 0)),
		RedisURL:               os.Getenv("REDIS_URL"),
		RedisEnabled:           envBoolOrDefault("REDIS_ENABLED", false),
		PoolReplenishThreshold: envIntOrDefault("POOL_REPLENISH_THRESHOLD", 500),
		PoolOptimalSize:        envIntOrDefault("POOL_OPTIMAL_SIZE", 5000),
		TreasuryPollInterval:   envIntOrDefault("TREASURY_POLL_INTERVAL", 60),

		// Backward-compatible aliases
		NoncePoolSize: poolSize,
		NonceLeaseTTL: leaseTTL,
	}

	if cfg.BSVPrivateKey == "" && cfg.XPRIV == "" {
		return nil, fmt.Errorf("one of XPRIV or BSV_PRIVATE_KEY is required")
	}
	if cfg.BSVNetwork != "mainnet" && cfg.BSVNetwork != "testnet" {
		return nil, fmt.Errorf("BSV_NETWORK must be 'mainnet' or 'testnet', got %q", cfg.BSVNetwork)
	}
	if cfg.FeeRate <= 0 {
		return nil, fmt.Errorf("FEE_RATE is required (set in .env, e.g. FEE_RATE=0.001 for BSV standard 1 sat/KB)")
	}
	if cfg.Broadcaster == "" {
		return nil, fmt.Errorf("BROADCASTER is required (set in .env, e.g. BROADCASTER=mock or BROADCASTER=woc)")
	}

	return cfg, nil
}

// IsMainnet returns true if the network is mainnet.
func (c *Config) IsMainnet() bool {
	return c.BSVNetwork == "mainnet"
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envBoolOrDefault(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}

func envFloatOrDefault(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
