// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//


package gatekeeper

import (
	"net/http"
	"time"

	"github.com/merkleworks/x402-bsv/internal/broadcast"
	"github.com/merkleworks/x402-bsv/internal/pool"
	"github.com/merkleworks/x402-bsv/internal/pricing"
	"github.com/merkleworks/x402-bsv/internal/replay"
)

// Config configures the gatekeeper middleware.
type Config struct {
	// MempoolChecker provides independent mempool verification (CRIT-04).
	// If nil, mempool check is skipped (backwards compat).
	MempoolChecker broadcast.MempoolChecker

	// ReplayCache provides early replay detection at the gatekeeper layer.
	// Defence-in-depth: the delegator also checks replay independently.
	ReplayCache *replay.Cache

	// ChallengeCache stores issued challenges for binding verification.
	ChallengeCache *ChallengeCache

	// NoncePool is the pool of 1-sat UTXOs used for replay-protection nonces.
	// Each 402 challenge leases a nonce from this pool. The proof transaction
	// must spend the nonce outpoint, providing Bitcoin-enforced single-use.
	NoncePool pool.Pool

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

	// SettlementRecorder is called on every successful 200 OK settlement.
	// If nil, settlement recording is skipped.
	SettlementRecorder SettlementRecorder

	// PaymentPool receives settlement revenue UTXOs. When a settlement succeeds
	// (200 OK), the payee output is added to this pool so it can later be swept
	// to the treasury address. If nil, settlement UTXO tracking is skipped.
	PaymentPool pool.Pool
}

// SettlementRecorder records successful payment settlements for revenue tracking.
// amountSats is the challenge price; txid/vout/satoshis/scriptHex describe the
// payee output UTXO so it can be swept to treasury without an indexer query.
type SettlementRecorder interface {
	RecordSettlement(amountSats int64, txid string, vout uint32, satoshis uint64, scriptHex string)
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
// Per x402.md §5, the proof object MUST contain the fields listed below.
type Proof struct {
	V               int            `json:"v"`                // integer per spec §5
	Scheme          string         `json:"scheme"`           // "bsv-tx-v1"
	ChallengeSHA256 string         `json:"challenge_sha256"` // SHA-256 of canonical challenge JSON
	Request         RequestBinding `json:"request"`          // request binding fields per spec §5
	Payment         Payment        `json:"payment"`          // nested per spec §5
	ClientSig       *ClientSig     `json:"client_sig,omitempty"`
}

// Payment contains the transaction data nested under "payment" per spec §5.
type Payment struct {
	TxID     string `json:"txid"`      // hex txid — MUST match decoded rawtx
	RawTxB64 string `json:"rawtx_b64"` // base64 (standard) encoded raw tx bytes
}

// RequestBinding contains the request parameters for binding validation.
// Per spec §5: method, path, query, req_headers_sha256, req_body_sha256.
type RequestBinding struct {
	Method           string `json:"method"`
	Path             string `json:"path"`
	Query            string `json:"query"`
	ReqHeadersSHA256 string `json:"req_headers_sha256"`
	ReqBodySHA256    string `json:"req_body_sha256"`
}

// ClientSig contains optional client signature (extension, not in v1.0 spec).
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
	ErrMempoolPending     ErrorCode = "payment_pending"
	ErrMempoolError       ErrorCode = "mempool_check_error"
	ErrInvalidBinding     ErrorCode = "invalid_binding"
	ErrDoubleSpend        ErrorCode = "double_spend"
	ErrInvalidProof       ErrorCode = "invalid_proof"
	ErrChallengeNotFound  ErrorCode = "challenge_not_found"
	ErrNonceMissing       ErrorCode = "nonce_missing"
	ErrNoUTXOsAvailable   ErrorCode = "no_utxos_available"
	ErrInternalError      ErrorCode = "internal_error"
)

// HTTPStatusForError maps spec error codes to HTTP status codes.
// Per x402.md §9:
//
//	Expired challenge       → 402 Payment Required
//	Nonce already spent     → 402 Payment Required
//	Invalid transaction     → 400 Bad Request
//	Request binding mismatch → 400 Bad Request
//	Insufficient payment    → 402 Payment Required
//	Unsupported scheme      → 400 Bad Request
func HTTPStatusForError(code ErrorCode) int {
	switch code {
	case ErrInvalidVersion, ErrInvalidScheme, ErrInvalidProof, ErrChallengeNotFound, ErrNonceMissing:
		return http.StatusBadRequest // 400
	case ErrInvalidBinding, ErrInvalidPayee:
		return http.StatusBadRequest // 400 — spec §9: "Request binding mismatch → 400" / "Invalid transaction → 400"
	case ErrExpiredChallenge, ErrInsufficientAmount, ErrDoubleSpend, ErrMempoolRejected:
		return http.StatusPaymentRequired // 402 — spec §9: "Nonce already spent → 402"
	case ErrMempoolPending:
		return http.StatusAccepted // 202
	case ErrNoUTXOsAvailable, ErrMempoolError:
		return http.StatusServiceUnavailable // 503
	default:
		return http.StatusInternalServerError // 500
	}
}
