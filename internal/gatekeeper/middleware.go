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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/merkleworks/x402-bsv/internal/challenge"
	"github.com/merkleworks/x402-bsv/internal/pool"
)

const (
	// ChallengeHeader is the HTTP header carrying the 402 challenge (per spec).
	ChallengeHeader = "X402-Challenge"

	// AcceptHeader signals supported payment schemes (per spec).
	AcceptHeader = "X402-Accept"

	// ProofHeader is the HTTP header carrying the client's payment proof (per spec).
	ProofHeader = "X402-Proof"

	// ReceiptHeader is the HTTP header carrying the payment receipt (per spec).
	ReceiptHeader = "X402-Receipt"

	// StatusHeader carries the mempool acceptance status (per spec).
	StatusHeader = "X402-Status"

	// AmountHeader carries the payment amount in satoshis (internal, for stats).
	AmountHeader = "X402-Amount-Sats"
)

// Middleware returns an http.Handler middleware that gates access behind x402 payment.
//
// Flow:
//  1. If no X402-Proof header → build challenge, return 402
//  2. If X402-Proof present → parse proof, verify tx structure, verify binding, check mempool, gate response
func Middleware(cfg Config) func(http.Handler) http.Handler {
	logger := slog.Default().With("component", "gatekeeper")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proofHeader := r.Header.Get(ProofHeader)

			if proofHeader == "" {
				// No proof — issue a 402 challenge
				handleChallenge(w, r, cfg, logger)
				return
			}

			// Proof present — verify and gate
			handleProof(w, r, next, proofHeader, cfg, logger)
		})
	}
}

// handleChallenge issues a 402 Payment Required response with a challenge.
func handleChallenge(w http.ResponseWriter, r *http.Request, cfg Config, logger *slog.Logger) {
	// Determine price
	amount, err := cfg.PricingFunc(r)
	if err != nil {
		logger.Error("pricing function failed", "error", err)
		writeError(w, http.StatusInternalServerError, string(ErrInternalError), "failed to determine price")
		return
	}

	// Lease a nonce UTXO for replay protection.
	// Per spec §4 / invariant N-1: nonce_utxo MUST identify a specific UTXO.
	// A challenge without a nonce UTXO has no replay protection.
	if cfg.NoncePool == nil {
		logger.Error("NoncePool not configured — cannot issue challenge (spec §4: nonce_utxo MUST be present)")
		writeError(w, HTTPStatusForError(ErrNoUTXOsAvailable), string(ErrNoUTXOsAvailable),
			"nonce pool not configured")
		return
	}

	nonceUTXO, err := cfg.NoncePool.Lease()
	if err != nil {
		logger.Error("no nonce UTXOs available", "error", err)
		writeError(w, HTTPStatusForError(ErrNoUTXOsAvailable), string(ErrNoUTXOsAvailable),
			"no nonce UTXOs available for challenge")
		return
	}
	nonceRef := &challenge.NonceRef{
		TxID:             nonceUTXO.TxID,
		Vout:             nonceUTXO.Vout,
		Satoshis:         nonceUTXO.Satoshis,
		LockingScriptHex: nonceUTXO.Script,
	}

	// Profile B: include pre-signed template if available
	var templateRef *challenge.TemplateRef
	if nonceUTXO.RawTxTemplate != "" {
		templateRef = &challenge.TemplateRef{
			RawTxHex:  nonceUTXO.RawTxTemplate,
			PriceSats: nonceUTXO.TemplatePriceSats,
		}
	}

	// Build the challenge
	bindHeaders := cfg.BindHeaders
	if len(bindHeaders) == 0 {
		bindHeaders = HeaderAllowlist
	}

	opts := challenge.BuildOptions{
		PayeeLockingScriptHex: cfg.PayeeLockingScriptHex,
		Amount:                amount,
		Network:               cfg.Network,
		TTL:                   cfg.ChallengeTTL,
		BindHeaders:           bindHeaders,
		NonceUTXO:             nonceRef,
		Template:              templateRef,
	}

	ch, err := challenge.Build(r, opts)
	if err != nil {
		logger.Error("challenge build failed", "error", err)
		writeError(w, http.StatusInternalServerError, string(ErrInternalError), "failed to build challenge")
		return
	}

	// Compute challenge hash for storage key
	challengeHash, err := challenge.ComputeHash(ch)
	if err != nil {
		logger.Error("challenge hash failed", "error", err)
		writeError(w, http.StatusInternalServerError, string(ErrInternalError), "failed to compute challenge hash")
		return
	}

	// Store challenge in cache for binding verification on proof submission
	if cfg.ChallengeCache != nil {
		cfg.ChallengeCache.Store(challengeHash, ch)
	}

	// Encode challenge for the header
	encoded, err := challenge.Encode(ch)
	if err != nil {
		logger.Error("challenge encode failed", "error", err)
		writeError(w, http.StatusInternalServerError, string(ErrInternalError), "failed to encode challenge")
		return
	}

	logger.Info("issuing 402 challenge",
		"path", r.URL.Path,
		"amount", amount,
		"challenge_hash", challengeHash,
		"nonce", nonceRefString(nonceRef),
	)

	// Return 402 with challenge per spec
	w.Header().Set(ChallengeHeader, encoded)
	w.Header().Set(AcceptHeader, challenge.Scheme)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"status":  402,
		"code":    "payment_required",
		"message": "Payment required. See X402-Challenge header.",
	})
}

// handleProof verifies a payment proof and gates the response.
//
// Verification order follows invariant V-1 (protocol-invariants.md):
//   1. Decode proof
//   2. Validate version + scheme
//   3. Recompute and validate challenge hash
//   4. Validate request binding
//   5. Check expiry
//   6. Decode transaction and verify txid
//   7. Verify nonce spend
//   8. Verify payee output
//   9. Verify mempool acceptance (if required)
//
// Defence-in-depth replay cache check runs before step 3 as a fast-path
// optimisation but is NOT a correctness gate — the settlement layer
// provides the authoritative replay protection (invariant R-1/R-2).
func handleProof(w http.ResponseWriter, r *http.Request, next http.Handler, proofHeader string, cfg Config, logger *slog.Logger) {
	// ── V-1 Step 1: Decode the proof object ─────────────────────────────
	proof, err := ParseProof(proofHeader)
	if err != nil {
		logger.Warn("invalid proof header", "error", err)
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), err.Error())
		return
	}

	// ── V-1 Step 2: Validate version and scheme ─────────────────────────
	if proof.V != challenge.Version {
		writeError(w, HTTPStatusForError(ErrInvalidVersion), string(ErrInvalidVersion),
			fmt.Sprintf("got version %d, want %d", proof.V, challenge.Version))
		return
	}
	if proof.Scheme != challenge.Scheme {
		writeError(w, HTTPStatusForError(ErrInvalidScheme), string(ErrInvalidScheme),
			fmt.Sprintf("got scheme %q, want %q", proof.Scheme, challenge.Scheme))
		return
	}

	// ── V-1 Step 3: Recompute and validate challenge hash ───────────────
	// Look up original challenge from cache using proof.ChallengeSHA256.
	var originalChallenge *challenge.Challenge
	if cfg.ChallengeCache != nil {
		originalChallenge = cfg.ChallengeCache.Lookup(proof.ChallengeSHA256)
	}

	// Defence-in-depth: replay cache fast-path (NOT a correctness gate).
	// Per R-2: "Servers MUST NOT rely on persistent nonce tracking for
	// replay protection correctness." This is an optimisation only — the
	// settlement layer provides authoritative replay protection via UTXO
	// single-spend (R-1). The fast-path avoids redundant tx decode/verify
	// for known-good idempotent re-serves and known-bad double-spends.
	if cfg.ReplayCache != nil && originalChallenge != nil && originalChallenge.NonceUTXO != nil {
		nonce := originalChallenge.NonceUTXO
		if existingTxID, _, found := cfg.ReplayCache.Check(nonce.TxID, nonce.Vout); found {
			// Compute txid early for this fast-path only.
			fastRawTx, decErr := base64.StdEncoding.DecodeString(proof.Payment.RawTxB64)
			if decErr == nil {
				fastTx, parseErr := transaction.NewTransactionFromBytes(fastRawTx)
				if parseErr == nil {
					fastTxID := fastTx.TxID().String()
					if constantTimeEqual(existingTxID, fastTxID) {
						// Same tx — idempotent re-serve, enforce mempool if required.
						logger.Info("idempotent re-serve",
							"txid", fastTxID,
							"nonce", fmt.Sprintf("%s:%d", nonce.TxID, nonce.Vout),
						)
						receiptHash := computeReceiptHash(fastTxID, proof.ChallengeSHA256)
						w.Header().Set(ReceiptHeader, receiptHash)
						w.Header().Set("X402-Receipt-Time", time.Now().UTC().Format(time.RFC3339))

						if originalChallenge.RequireMempoolAccept {
							if cfg.MempoolChecker == nil {
								logger.Error("require_mempool_accept but no MempoolChecker configured", "txid", fastTxID)
								w.Header().Set(StatusHeader, "error")
								writeError(w, HTTPStatusForError(ErrMempoolError), string(ErrMempoolError),
									"mempool verification required but not configured")
								return
							}
							visible, doubleSpend, mErr := cfg.MempoolChecker.CheckMempool(fastTxID)
							if mErr != nil {
								logger.Error("mempool check failed on re-serve", "txid", fastTxID, "error", mErr)
								w.Header().Set(StatusHeader, "error")
								writeError(w, HTTPStatusForError(ErrMempoolError), string(ErrMempoolError),
									fmt.Sprintf("mempool verification failed: %s", mErr))
								return
							}
							if doubleSpend {
								logger.Warn("mempool double-spend on re-serve", "txid", fastTxID)
								w.Header().Set(StatusHeader, "rejected")
								writeError(w, HTTPStatusForError(ErrDoubleSpend), string(ErrDoubleSpend),
									"transaction rejected by mempool as double-spend")
								return
							}
							if !visible {
								logger.Info("re-serve: payment pending (not yet in mempool)", "txid", fastTxID)
								w.Header().Set(StatusHeader, "pending")
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusAccepted)
								json.NewEncoder(w).Encode(map[string]any{
									"status":  202,
									"code":    string(ErrMempoolPending),
									"message": "transaction not yet visible in mempool",
									"txid":    fastTxID,
								})
								return
							}
						}

						// Re-verify request binding even on re-serve.
						// Without this, an attacker could replay a valid proof
						// against a different endpoint on the same server.
						reserveBindHeaders := cfg.BindHeaders
						if len(reserveBindHeaders) == 0 {
							reserveBindHeaders = HeaderAllowlist
						}
						if bindErr := challenge.VerifyBinding(originalChallenge, r, reserveBindHeaders); bindErr != nil {
							logger.Warn("binding mismatch on re-serve", "error", bindErr)
							writeError(w, HTTPStatusForError(ErrInvalidBinding), string(ErrInvalidBinding), bindErr.Error())
							return
						}

						// Idempotent re-serve: the resource was already executed
						// on the original proof submission. Return receipt-only
						// confirmation WITHOUT re-executing the downstream handler.
						// This prevents duplicate execution under concurrent retry.
						if cfg.ChallengeCache != nil {
							cfg.ChallengeCache.Delete(proof.ChallengeSHA256)
						}
						if cfg.NoncePool != nil {
							cfg.NoncePool.MarkSpent(nonce.TxID, nonce.Vout)
						}
						w.Header().Set(StatusHeader, "accepted")
						w.Header().Set(AmountHeader, fmt.Sprintf("%d", originalChallenge.AmountSats))
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						json.NewEncoder(w).Encode(map[string]any{
							"status":  200,
							"code":    "already_settled",
							"message": "payment already accepted",
							"txid":    fastTxID,
						})
						return
					}
					// Different txid — hint at double-spend but fall through to
					// full V-1 verification. The settlement layer is authoritative.
					logger.Warn("replay cache hint: possible double-spend",
						"nonce", fmt.Sprintf("%s:%d", nonce.TxID, nonce.Vout),
						"existing_txid", existingTxID,
						"proof_txid", fastTxID,
					)
					// Fall through to full verification — do NOT reject based on
					// cache alone (invariant R-2).
				}
			}
		}
	}

	if originalChallenge == nil {
		writeError(w, HTTPStatusForError(ErrChallengeNotFound), string(ErrChallengeNotFound),
			"challenge not found or expired")
		return
	}

	// Per spec §7: "The server MUST recompute challenge_sha256 and MUST compare
	// it to the provided value."
	recomputedHash, err := challenge.ComputeHash(originalChallenge)
	if err != nil {
		logger.Error("failed to recompute challenge hash", "error", err)
		writeError(w, http.StatusInternalServerError, string(ErrInternalError), "failed to recompute challenge hash")
		return
	}
	if !constantTimeEqual(recomputedHash, proof.ChallengeSHA256) {
		logger.Warn("challenge_sha256 mismatch",
			"proof", proof.ChallengeSHA256,
			"recomputed", recomputedHash,
		)
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof),
			"challenge_sha256 does not match recomputed value")
		return
	}

	// ── V-1 Step 4: Validate request binding ────────────────────────────
	bindHeaders := cfg.BindHeaders
	if len(bindHeaders) == 0 {
		bindHeaders = HeaderAllowlist
	}
	if err := challenge.VerifyBinding(originalChallenge, r, bindHeaders); err != nil {
		logger.Warn("binding mismatch", "error", err)
		writeError(w, HTTPStatusForError(ErrInvalidBinding), string(ErrInvalidBinding), err.Error())
		return
	}

	// ── V-1 Step 5: Check expiration ────────────────────────────────────
	// Per spec §7: "The server MUST reject proofs for challenges where the
	// current time is strictly greater than expires_at."
	if time.Now().Unix() > originalChallenge.ExpiresAt {
		writeError(w, HTTPStatusForError(ErrExpiredChallenge), string(ErrExpiredChallenge),
			"challenge has expired")
		return
	}

	// ── V-1 Step 6: Decode the transaction and verify txid ──────────────
	rawTxBytes, err := base64.StdEncoding.DecodeString(proof.Payment.RawTxB64)
	if err != nil {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), "invalid payment.rawtx_b64")
		return
	}

	tx, err := transaction.NewTransactionFromBytes(rawTxBytes)
	if err != nil {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), "cannot parse transaction")
		return
	}

	// Per spec §5/§6: payment.txid MUST match the decoded transaction bytes.
	computedTxID := tx.TxID().String()
	if !constantTimeEqual(proof.Payment.TxID, computedTxID) {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof),
			fmt.Sprintf("txid mismatch: proof=%s, computed=%s", proof.Payment.TxID, computedTxID))
		return
	}

	if tx.InputCount() < 1 {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), "transaction has no inputs")
		return
	}

	// ── V-1 Step 7: Verify nonce UTXO is spent ─────────────────────────
	// Per spec §4: nonce_utxo MUST identify a specific UTXO.
	// Per spec §6: "The transaction MUST include an input whose outpoint exactly
	// matches the nonce_utxo."
	if originalChallenge.NonceUTXO == nil {
		logger.Error("challenge missing nonce_utxo (violates spec §4)")
		writeError(w, HTTPStatusForError(ErrNonceMissing), string(ErrNonceMissing),
			"challenge has no nonce_utxo")
		return
	}
	if err := verifyNonceSpend(tx, originalChallenge.NonceUTXO); err != nil {
		logger.Warn("nonce spend verification failed", "error", err)
		writeError(w, HTTPStatusForError(ErrNonceMissing), string(ErrNonceMissing), err.Error())
		return
	}

	// Profile B (template mode): verify nonce is at input[0].
	// SIGHASH_SINGLE binds the signature to output[input_index], so the nonce
	// input at index 0 commits to output[0] (the payee output).
	if originalChallenge.Template != nil {
		if err := verifyNonceAtInput0(tx, originalChallenge.NonceUTXO); err != nil {
			logger.Warn("nonce position verification failed (template mode)", "error", err)
			writeError(w, HTTPStatusForError(ErrNonceMissing), string(ErrNonceMissing), err.Error())
			return
		}
	}

	// ── V-1 Step 8: Verify the payment output ───────────────────────────
	if err := verifyPayeeOutput(tx, originalChallenge.PayeeLockingScriptHex, originalChallenge.AmountSats); err != nil {
		logger.Warn("payee output verification failed", "error", err)
		writeError(w, HTTPStatusForError(ErrInvalidPayee), string(ErrInvalidPayee), err.Error())
		return
	}

	// Step 15: Atomically reserve this nonce in the replay cache to prevent
	// concurrent duplicate execution. TryReserve is atomic (single mutex) —
	// if two goroutines race, exactly one wins the reservation.
	// `reserved` is hoisted to function scope so the commit at the end can clear it.
	var reserved bool
	if cfg.ReplayCache != nil && originalChallenge.NonceUTXO != nil {
		nonce := originalChallenge.NonceUTXO
		var existingTxID string
		var pending bool
		reserved, existingTxID, pending = cfg.ReplayCache.TryReserve(nonce.TxID, nonce.Vout, proof.ChallengeSHA256)
		if !reserved {
			if pending {
				// Another goroutine is processing this nonce right now.
				// Return 202 to tell the client to retry shortly.
				logger.Info("concurrent proof processing in progress",
					"nonce", fmt.Sprintf("%s:%d", nonce.TxID, nonce.Vout))
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(map[string]any{
					"status":  202,
					"code":    string(ErrMempoolPending),
					"message": "proof verification in progress, retry shortly",
				})
				return
			}
			// Already committed — idempotent receipt or double-spend.
			if existingTxID != "" {
				if constantTimeEqual(existingTxID, computedTxID) {
					// Same txid already committed — return receipt-only
					// response without re-executing downstream handler.
					receiptHash := computeReceiptHash(computedTxID, proof.ChallengeSHA256)
					w.Header().Set(ReceiptHeader, receiptHash)
					w.Header().Set(StatusHeader, "accepted")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]any{
						"status":  200,
						"code":    "already_settled",
						"message": "payment already accepted",
						"txid":    computedTxID,
					})
					return
				}
				logger.Warn("nonce already committed with different txid",
					"nonce", fmt.Sprintf("%s:%d", nonce.TxID, nonce.Vout),
					"existing_txid", existingTxID,
					"proof_txid", computedTxID,
				)
				// Fall through — mempool checker will catch the double-spend.
			}
		}
		// If reserved, we own this nonce slot. Commit after mempool acceptance,
		// or release on failure.
		defer func() {
			if reserved {
				// If we reach here without having committed, release the reservation.
				// This happens on 202 (pending), 503 (error), etc.
				cfg.ReplayCache.Release(nonce.TxID, nonce.Vout, proof.ChallengeSHA256)
			}
		}()
		// Mark reserved=false after successful commit to prevent deferred release.
		_ = reserved // used by deferred function
	}

	// Step 15b: Mark nonce UTXO as spent immediately after replay-cache recording.
	// This must happen on BOTH the 200 and 202 paths. Once a valid proof has been
	// recorded, the nonce is consumed regardless of mempool visibility — the
	// transaction has been built and broadcast, so this nonce's on-chain outpoint
	// is spent. Without this, the 202 path leaves the nonce as merely "leased",
	// and the lease reclaim loop eventually returns it to "available" even though
	// it's spent on-chain, causing txn-mempool-conflict on the next flow.
	if cfg.NoncePool != nil && originalChallenge.NonceUTXO != nil {
		cfg.NoncePool.MarkSpent(originalChallenge.NonceUTXO.TxID, originalChallenge.NonceUTXO.Vout)
	}

	// Step 16: Mempool acceptance matrix (CRIT-04)
	// Per Protocol-Spec:
	//   200 = mempool-visible → serve protected response
	//   202 = pending (not visible yet) → do NOT serve
	//   409 = explicit double-spend → do NOT serve
	//   503 = checker error → do NOT serve
	receiptHash := computeReceiptHash(computedTxID, proof.ChallengeSHA256)
	w.Header().Set(ReceiptHeader, receiptHash)
	w.Header().Set("X402-Receipt-Time", time.Now().UTC().Format(time.RFC3339))

	if originalChallenge.RequireMempoolAccept && cfg.MempoolChecker == nil {
		// Hard failure: challenge requires mempool verification but no checker configured
		logger.Error("require_mempool_accept but no MempoolChecker configured", "txid", computedTxID)
		w.Header().Set(StatusHeader, "error")
		writeError(w, HTTPStatusForError(ErrMempoolError), string(ErrMempoolError),
			"mempool verification required but not configured")
		return
	}

	if cfg.MempoolChecker != nil && originalChallenge.RequireMempoolAccept {
		visible, doubleSpend, err := cfg.MempoolChecker.CheckMempool(computedTxID)
		if err != nil {
			// 503 — mempool check failed
			logger.Error("mempool check failed", "txid", computedTxID, "error", err)
			w.Header().Set(StatusHeader, "error")
			writeError(w, HTTPStatusForError(ErrMempoolError), string(ErrMempoolError),
				fmt.Sprintf("mempool verification failed: %s", err))
			return
		}

		if doubleSpend {
			// 409 — explicit double-spend detected
			logger.Warn("mempool double-spend detected", "txid", computedTxID)
			w.Header().Set(StatusHeader, "rejected")
			writeError(w, HTTPStatusForError(ErrDoubleSpend), string(ErrDoubleSpend),
				"transaction rejected by mempool as double-spend")
			return
		}

		if !visible {
			// 202 — tx not yet visible, payment acknowledged but not confirmed
			logger.Info("payment pending (not yet in mempool)", "txid", computedTxID)
			w.Header().Set(StatusHeader, "pending")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{
				"status":  202,
				"code":    string(ErrMempoolPending),
				"message": "transaction not yet visible in mempool",
				"txid":    computedTxID,
			})
			return
		}

		// 200 — tx visible in mempool, serve protected response
		w.Header().Set(StatusHeader, "accepted")
	}

	// Step 14b: Delete challenge from cache now that payment is fully accepted (single-use).
	// This runs only on the 200 path (mempool-visible or no mempool check required).
	if cfg.ChallengeCache != nil {
		cfg.ChallengeCache.Delete(proof.ChallengeSHA256)
	}

	// Step 14c: Mark nonce UTXO as spent in the pool so it's never re-leased.
	// Critical: without this, the nonce lease expires → returns to "available" →
	// next challenge picks the same nonce → broadcast fails ("Missing inputs")
	// because the nonce UTXO was already spent on-chain.
	if cfg.NoncePool != nil && originalChallenge.NonceUTXO != nil {
		cfg.NoncePool.MarkSpent(originalChallenge.NonceUTXO.TxID, originalChallenge.NonceUTXO.Vout)
	}

	// Set payment amount header for stats collector (read by loggingMiddleware)
	w.Header().Set(AmountHeader, fmt.Sprintf("%d", originalChallenge.AmountSats))

	// Find the payee output for settlement tracking.
	// In Profile B, the payee output is always at vout 0 (SIGHASH_SINGLE at input[0]
	// commits to output[0]). We scan all outputs to handle both profiles correctly.
	var payeeVout uint32
	var payeeSats uint64
	var payeeScript string
	for idx, out := range tx.Outputs {
		scriptHex := hex.EncodeToString(*out.LockingScript)
		if constantTimeEqual(scriptHex, cfg.PayeeLockingScriptHex) && int64(out.Satoshis) >= originalChallenge.AmountSats {
			payeeVout = uint32(idx)
			payeeSats = out.Satoshis
			payeeScript = scriptHex
			break
		}
	}

	// Record settlement revenue + UTXO details (persistent — Redis-backed).
	// The UTXO info allows sweep-to-treasury without an indexer query.
	if cfg.SettlementRecorder != nil {
		cfg.SettlementRecorder.RecordSettlement(originalChallenge.AmountSats, computedTxID, payeeVout, payeeSats, payeeScript)
	}

	// Track the settlement output in the payment pool so it can be swept to treasury.
	if cfg.PaymentPool != nil && payeeScript != "" {
		cfg.PaymentPool.AddExisting([]pool.UTXO{{
			TxID:     computedTxID,
			Vout:     payeeVout,
			Script:   payeeScript,
			Satoshis: payeeSats,
		}})
		logger.Debug("tracked settlement output in payment pool",
			"txid", computedTxID,
			"vout", payeeVout,
			"satoshis", payeeSats,
		)
	}

	// Commit the replay cache reservation now that payment is fully accepted.
	// This transitions the pending entry to committed (spendTxID set), making
	// it visible to subsequent Check() calls for idempotent re-serve.
	// Setting reserved=false prevents the deferred Release from firing.
	if cfg.ReplayCache != nil && originalChallenge.NonceUTXO != nil {
		nonce := originalChallenge.NonceUTXO
		if commitErr := cfg.ReplayCache.Commit(nonce.TxID, nonce.Vout, proof.ChallengeSHA256, computedTxID); commitErr != nil {
			// Non-fatal: replay cache commit failed but payment is valid.
			// The settlement layer provides authoritative replay protection.
			logger.Warn("replay cache commit failed (non-fatal)", "error", commitErr)
		}
		reserved = false // prevent deferred Release
	}

	logger.Info("payment accepted",
		"txid", computedTxID,
		"path", r.URL.Path,
		"amount_sats", originalChallenge.AmountSats,
		"nonce", nonceRefString(originalChallenge.NonceUTXO),
		"receipt", receiptHash,
	)

	next.ServeHTTP(w, r)
}

// verifyNonceSpend checks that the transaction spends the nonce UTXO
// specified in the challenge. This is the core replay protection mechanism:
// Bitcoin consensus guarantees that an outpoint can only be spent once.
// Uses constant-time comparison for txid.
func verifyNonceSpend(tx *transaction.Transaction, nonce *challenge.NonceRef) error {
	if nonce == nil {
		return fmt.Errorf("challenge has no nonce_utxo")
	}
	for _, input := range tx.Inputs {
		if input.SourceTXID != nil &&
			constantTimeEqual(input.SourceTXID.String(), nonce.TxID) &&
			input.SourceTxOutIndex == nonce.Vout {
			return nil // found the nonce input
		}
	}
	return fmt.Errorf("transaction does not spend nonce outpoint %s:%d", nonce.TxID, nonce.Vout)
}

// verifyNonceAtInput0 checks that the nonce outpoint is consumed at input
// index 0. Required for Profile B (template mode) because the gateway's
// SIGHASH_SINGLE signature on input 0 commits to output[0] (the payee output).
// Uses constant-time comparison for txid.
func verifyNonceAtInput0(tx *transaction.Transaction, nonce *challenge.NonceRef) error {
	if nonce == nil {
		return fmt.Errorf("challenge has no nonce_utxo")
	}
	if tx.InputCount() < 1 {
		return fmt.Errorf("transaction has no inputs")
	}
	input0 := tx.Inputs[0]
	if input0.SourceTXID == nil ||
		!constantTimeEqual(input0.SourceTXID.String(), nonce.TxID) ||
		input0.SourceTxOutIndex != nonce.Vout {
		return fmt.Errorf("template mode requires nonce at input[0], but input[0] is %s:%d (expected %s:%d)",
			input0.SourceTXID, input0.SourceTxOutIndex, nonce.TxID, nonce.Vout)
	}
	return nil
}

// verifyPayeeOutput checks that the transaction has at least one output
// paying >= minAmount to the expected payee locking script.
// Uses constant-time comparison for script hex.
func verifyPayeeOutput(tx *transaction.Transaction, expectedScriptHex string, minAmount int64) error {
	for _, out := range tx.Outputs {
		scriptHex := hex.EncodeToString(*out.LockingScript)
		if constantTimeEqual(scriptHex, expectedScriptHex) && int64(out.Satoshis) >= minAmount {
			return nil // found valid payee output
		}
	}
	return fmt.Errorf("no output paying >= %d sats to expected payee", minAmount)
}

// constantTimeEqual compares two strings in constant time to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func nonceRefString(n *challenge.NonceRef) string {
	if n == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s:%d", n.TxID, n.Vout)
}

func computeReceiptHash(txid, challengeHash string) string {
	h := sha256.Sum256([]byte(txid + ":" + challengeHash))
	return hex.EncodeToString(h[:])
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"status":  status,
		"code":    code,
		"message": message,
	})
}
