package gatekeeper

import (
	"net/http"
	"time"

	deleg "github.com/merkle-works/x402-gateway/internal/delegator"
	"github.com/merkle-works/x402-gateway/internal/pricing"
	"github.com/merkle-works/x402-gateway/internal/replay"
)

// Config configures the gatekeeper middleware.
type Config struct {
	// Delegator handles transaction finalization and broadcasting.
	Delegator *deleg.Delegator

	// ReplayCache provides early replay detection at the gatekeeper layer.
	// Defence-in-depth: the delegator also checks replay independently.
	ReplayCache *replay.Cache

	// ChallengeCache stores issued challenges for binding verification.
	ChallengeCache *ChallengeCache

	// PayeeLockingScriptHex is the hex-encoded locking script for payments.
	PayeeLockingScriptHex string

	// Network is "mainnet" or "testnet" (server-side only, not on wire).
	Network string

	// PricingFunc determines the price for each request.
	PricingFunc pricing.Func

	// ChallengeTTL is how long a challenge remains valid.
	ChallengeTTL time.Duration

	// BindHeaders specifies which request headers to include in the challenge binding.
	// Defaults to HeaderAllowlist if empty.
	BindHeaders []string
}

// HeaderAllowlist defines headers included in canonical request hash (per spec).
var HeaderAllowlist = []string{
	"accept",
	"content-length",
	"content-type",
	"x402-client",
	"x402-idempotency-key",
}

// Proof is the client's payment proof submitted in the X402-Proof header.
type Proof struct {
	V               string         `json:"v"`
	Scheme          string         `json:"scheme"`
	TxID            string         `json:"txid"`
	RawTxB64        string         `json:"rawtx_b64"`
	ChallengeSHA256 string         `json:"challenge_sha256"`
	Request         RequestBinding `json:"request"`
	ClientSig       *ClientSig     `json:"client_sig,omitempty"`
}

// RequestBinding contains the request parameters for binding validation.
type RequestBinding struct {
	Domain           string `json:"domain"`
	Method           string `json:"method"`
	Path             string `json:"path"`
	Query            string `json:"query"`
	ReqHeadersSHA256 string `json:"req_headers_sha256"`
	ReqBodySHA256    string `json:"req_body_sha256"`
}

// ClientSig contains optional client signature (required in v1.1).
type ClientSig struct {
	Alg          string `json:"alg"`
	PubkeyHex    string `json:"pubkey_hex"`
	SignatureHex string `json:"signature_hex"`
}

// ---------------------------------------------------------------------------
// Spec error codes
// ---------------------------------------------------------------------------

// ErrorCode is a spec-defined error identifier.
type ErrorCode string

const (
	ErrInvalidVersion     ErrorCode = "invalid_version"
	ErrInvalidScheme      ErrorCode = "invalid_scheme"
	ErrInvalidPayee       ErrorCode = "invalid_payee"
	ErrInsufficientAmount ErrorCode = "insufficient_amount"
	ErrExpiredChallenge   ErrorCode = "expired_challenge"
	ErrMempoolRejected    ErrorCode = "mempool_rejected"
	ErrInvalidBinding     ErrorCode = "invalid_binding"
	ErrDoubleSpend        ErrorCode = "double_spend"
	ErrInvalidProof       ErrorCode = "invalid_proof"
	ErrChallengeNotFound  ErrorCode = "challenge_not_found"
	ErrNoUTXOsAvailable   ErrorCode = "no_utxos_available"
	ErrInternalError      ErrorCode = "internal_error"
)

// HTTPStatusForError maps spec error codes to HTTP status codes.
func HTTPStatusForError(code ErrorCode) int {
	switch code {
	case ErrInvalidVersion, ErrInvalidScheme, ErrInvalidProof, ErrChallengeNotFound:
		return http.StatusBadRequest
	case ErrExpiredChallenge, ErrMempoolRejected, ErrInsufficientAmount:
		return http.StatusPaymentRequired
	case ErrInvalidBinding, ErrInvalidPayee:
		return http.StatusForbidden
	case ErrDoubleSpend:
		return http.StatusConflict
	case ErrNoUTXOsAvailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
