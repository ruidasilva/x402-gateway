package gatekeeper

import (
	"time"

	deleg "github.com/merkle-works/x402-gateway/internal/delegator"
	"github.com/merkle-works/x402-gateway/internal/nonce"
	"github.com/merkle-works/x402-gateway/internal/pricing"
)

// Config configures the gatekeeper middleware.
type Config struct {
	// Delegator handles transaction finalization and broadcasting.
	Delegator *deleg.Delegator

	// NoncePool provides nonce UTXOs for challenges.
	NoncePool *nonce.Pool

	// PayeeAddress is the BSV address that receives payments.
	PayeeAddress string

	// Network is "mainnet" or "testnet".
	Network string

	// PricingFunc determines the price for each request.
	PricingFunc pricing.Func

	// ChallengeTTL is how long a challenge remains valid.
	ChallengeTTL time.Duration

	// BindHeaders specifies which request headers to include in the challenge binding.
	BindHeaders []string
}

// Proof is the client's payment proof submitted in the X-402-Proof header.
type Proof struct {
	PartialTxHex  string `json:"partial_tx"`
	ChallengeHash string `json:"challenge_hash"`
}
