package gatekeeper

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/merkle-works/x402-gateway/internal/challenge"
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
)

// Middleware returns an http.Handler middleware that gates access behind x402 payment.
//
// Flow:
//  1. If no X402-Proof header → build challenge, return 402
//  2. If X402-Proof present → parse proof, verify tx structure, verify binding, gate response
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
func handleProof(w http.ResponseWriter, r *http.Request, next http.Handler, proofHeader string, cfg Config, logger *slog.Logger) {
	// Step 1: Parse the proof
	proof, err := ParseProof(proofHeader)
	if err != nil {
		logger.Warn("invalid proof header", "error", err)
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), err.Error())
		return
	}

	// Step 2: Validate version and scheme
	if proof.V != challenge.Version {
		writeError(w, HTTPStatusForError(ErrInvalidVersion), string(ErrInvalidVersion),
			fmt.Sprintf("got version %q, want %q", proof.V, challenge.Version))
		return
	}
	if proof.Scheme != challenge.Scheme {
		writeError(w, HTTPStatusForError(ErrInvalidScheme), string(ErrInvalidScheme),
			fmt.Sprintf("got scheme %q, want %q", proof.Scheme, challenge.Scheme))
		return
	}

	// Step 3: Decode the raw transaction from base64
	rawTxBytes, err := base64.StdEncoding.DecodeString(proof.RawTxB64)
	if err != nil {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), "invalid rawtx_b64")
		return
	}

	tx, err := transaction.NewTransactionFromBytes(rawTxBytes)
	if err != nil {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), "cannot parse transaction")
		return
	}

	// Step 4: Compute txid and compare to proof.txid
	computedTxID := tx.TxID().String()
	if proof.TxID != "" && proof.TxID != computedTxID {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof),
			fmt.Sprintf("txid mismatch: proof=%s, computed=%s", proof.TxID, computedTxID))
		return
	}

	// Step 5: Verify transaction has inputs
	if tx.InputCount() < 1 {
		writeError(w, HTTPStatusForError(ErrInvalidProof), string(ErrInvalidProof), "transaction has no inputs")
		return
	}

	// Step 6: Replay check — keyed on challenge hash
	if cfg.ReplayCache != nil {
		if existingTxID, found := cfg.ReplayCache.Check(proof.ChallengeSHA256); found {
			if existingTxID != computedTxID {
				logger.Warn("replay detected at gatekeeper",
					"challenge_hash", proof.ChallengeSHA256,
					"existing_txid", existingTxID,
					"proof_txid", computedTxID,
				)
				writeError(w, HTTPStatusForError(ErrDoubleSpend), string(ErrDoubleSpend),
					fmt.Sprintf("challenge already settled in tx %s", existingTxID))
				return
			}
			// Same txid — delegator already processed this; allow through
		}
	}

	// Step 7: Look up original challenge for binding verification
	if cfg.ChallengeCache != nil {
		originalChallenge := cfg.ChallengeCache.Lookup(proof.ChallengeSHA256)
		if originalChallenge == nil {
			writeError(w, HTTPStatusForError(ErrChallengeNotFound), string(ErrChallengeNotFound),
				"challenge not found or expired")
			return
		}

		// Validate scheme and version on the challenge too
		if err := challenge.ValidateSchemeVersion(originalChallenge); err != nil {
			logger.Warn("challenge scheme/version mismatch", "error", err)
			if originalChallenge.Scheme != challenge.Scheme {
				writeError(w, HTTPStatusForError(ErrInvalidScheme), string(ErrInvalidScheme), err.Error())
			} else {
				writeError(w, HTTPStatusForError(ErrInvalidVersion), string(ErrInvalidVersion), err.Error())
			}
			return
		}

		// Check challenge expiry
		if originalChallenge.ExpiresAt <= time.Now().Unix() {
			writeError(w, HTTPStatusForError(ErrExpiredChallenge), string(ErrExpiredChallenge),
				"challenge has expired")
			return
		}

		// Verify request binding
		bindHeaders := cfg.BindHeaders
		if len(bindHeaders) == 0 {
			bindHeaders = HeaderAllowlist
		}
		if err := challenge.VerifyBinding(originalChallenge, r, bindHeaders); err != nil {
			logger.Warn("binding mismatch", "error", err)
			writeError(w, HTTPStatusForError(ErrInvalidBinding), string(ErrInvalidBinding), err.Error())
			return
		}

		// Confirm tx has output paying >= amount_sats to payee
		if err := verifyPayeeOutput(tx, originalChallenge.PayeeLockingScriptHex, originalChallenge.AmountSats); err != nil {
			logger.Warn("payee output verification failed", "error", err)
			writeError(w, HTTPStatusForError(ErrInvalidPayee), string(ErrInvalidPayee), err.Error())
			return
		}

		// Delete challenge from cache after successful verification (single-use)
		cfg.ChallengeCache.Delete(proof.ChallengeSHA256)
	}

	// Record in replay cache
	if cfg.ReplayCache != nil {
		cfg.ReplayCache.Record(proof.ChallengeSHA256, computedTxID)
	}

	// Success — add receipt header and pass through to the protected handler
	receiptHash := computeReceiptHash(computedTxID, proof.ChallengeSHA256)
	w.Header().Set(ReceiptHeader, receiptHash)

	logger.Info("payment accepted",
		"txid", computedTxID,
		"path", r.URL.Path,
		"receipt", receiptHash,
	)

	next.ServeHTTP(w, r)
}

// verifyPayeeOutput checks that the transaction has at least one output
// paying >= minAmount to the expected payee locking script.
func verifyPayeeOutput(tx *transaction.Transaction, expectedScriptHex string, minAmount int64) error {
	for _, out := range tx.Outputs {
		scriptHex := hex.EncodeToString(*out.LockingScript)
		if scriptHex == expectedScriptHex && int64(out.Satoshis) >= minAmount {
			return nil // found valid payee output
		}
	}
	return fmt.Errorf("no output paying >= %d sats to expected payee", minAmount)
}

func computeReceiptHash(txid, challengeHash string) string {
	h := sha256.Sum256([]byte(txid + ":" + challengeHash + ":" + timeNowString()))
	return hex.EncodeToString(h[:])
}

func timeNowString() string {
	return time.Now().UTC().Format(time.RFC3339)
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
