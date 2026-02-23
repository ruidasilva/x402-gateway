package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all gateway configuration.
type Config struct {
	// BSV private key in WIF format
	BSVPrivateKey string

	// BSV network: "mainnet" or "testnet"
	BSVNetwork string

	// Service payee address
	PayeeAddress string

	// HTTP server port
	Port int

	// Nonce pool initial size
	NoncePoolSize int

	// Nonce lease TTL
	NonceLeaseTTL time.Duration

	// Fee rate in sat/byte
	FeeRate float64

	// Broadcaster type: "woc" or "arc"
	Broadcaster string

	// Daily fee budget in satoshis (0 = unlimited)
	DailyFeeBudget uint64
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		BSVPrivateKey:  os.Getenv("BSV_PRIVATE_KEY"),
		BSVNetwork:     envOrDefault("BSV_NETWORK", "testnet"),
		PayeeAddress:   os.Getenv("PAYEE_ADDRESS"),
		Port:           envIntOrDefault("PORT", 8402),
		NoncePoolSize:  envIntOrDefault("NONCE_POOL_SIZE", 100),
		NonceLeaseTTL:  time.Duration(envIntOrDefault("NONCE_LEASE_TTL", 300)) * time.Second,
		FeeRate:        envFloatOrDefault("FEE_RATE", 1.0),
		Broadcaster:    envOrDefault("BROADCASTER", "woc"),
		DailyFeeBudget: uint64(envIntOrDefault("DAILY_FEE_BUDGET", 0)),
	}

	if cfg.BSVPrivateKey == "" {
		return nil, fmt.Errorf("BSV_PRIVATE_KEY is required")
	}
	if cfg.BSVNetwork != "mainnet" && cfg.BSVNetwork != "testnet" {
		return nil, fmt.Errorf("BSV_NETWORK must be 'mainnet' or 'testnet', got %q", cfg.BSVNetwork)
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

func envFloatOrDefault(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
