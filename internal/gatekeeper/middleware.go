package gatekeeper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/merkle-works/x402-gateway/internal/challenge"
	deleg "github.com/merkle-works/x402-gateway/internal/delegator"
)

const (
	// ProofHeader is the HTTP header carrying the client's payment proof.
	ProofHeader = "X-402-Proof"

	// ReceiptHeader is the HTTP header carrying the payment receipt.
	ReceiptHeader = "X-402-Receipt"

	// AuthenticateHeader is the standard WWW-Authenticate header.
	AuthenticateHeader = "WWW-Authenticate"
)

// Middleware returns an http.Handler middleware that gates access behind x402 payment.
//
// Flow:
//  1. If no X-402-Proof header → lease nonce, build challenge, return 402
//  2. If X-402-Proof present → parse proof, validate, delegate, pass through on success
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

			// Proof present — validate and delegate
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
		writeError(w, http.StatusInternalServerError, "PRICING_ERROR", "failed to determine price")
		return
	}

	// Lease a nonce UTXO
	nonceUTXO, err := cfg.NoncePool.Lease()
	if err != nil {
		logger.Error("nonce lease failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "NONCE_EXHAUSTED", "no nonces available")
		return
	}

	// Build the challenge
	opts := challenge.BuildOptions{
		PayeeAddress: cfg.PayeeAddress,
		Amount:       amount,
		Network:      cfg.Network,
		TTL:          cfg.ChallengeTTL,
		BindHeaders:  cfg.BindHeaders,
	}

	ch, err := challenge.Build(r, nonceUTXO, opts)
	if err != nil {
		logger.Error("challenge build failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CHALLENGE_ERROR", "failed to build challenge")
		return
	}

	// Encode challenge for the header
	encoded, err := challenge.Encode(ch)
	if err != nil {
		logger.Error("challenge encode failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CHALLENGE_ERROR", "failed to encode challenge")
		return
	}

	logger.Info("issuing 402 challenge",
		"path", r.URL.Path,
		"amount", amount,
		"nonce", nonceUTXO.Outpoint(),
		"challenge_hash", ch.ChallengeSHA256,
	)

	// Return 402 with challenge
	w.Header().Set(AuthenticateHeader, "X402 "+encoded)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    402,
		"code":      "PAYMENT_REQUIRED",
		"message":   "Payment required. See WWW-Authenticate header for challenge.",
		"challenge": ch,
	})
}

// handleProof validates a payment proof and delegates the transaction.
func handleProof(w http.ResponseWriter, r *http.Request, next http.Handler, proofHeader string, cfg Config, logger *slog.Logger) {
	// Parse the proof
	proof, err := ParseProof(proofHeader)
	if err != nil {
		logger.Warn("invalid proof header", "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_PROOF", err.Error())
		return
	}

	// Extract the nonce outpoint from the partial transaction using the SDK
	nonceTxID, nonceVout, err := extractNonceOutpoint(proof.PartialTxHex)
	if err != nil {
		logger.Warn("cannot extract nonce from partial tx", "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_PROOF", "cannot parse partial transaction")
		return
	}

	// Look up the nonce in our pool to get its metadata
	nonceUTXO := cfg.NoncePool.Lookup(nonceTxID, nonceVout)
	if nonceUTXO == nil {
		writeError(w, http.StatusPaymentRequired, "WRONG_NONCE", "nonce not from this gateway")
		return
	}

	// Determine the expected price for this request
	amount, err := cfg.PricingFunc(r)
	if err != nil {
		logger.Error("pricing function failed", "error", err)
		writeError(w, http.StatusInternalServerError, "PRICING_ERROR", "failed to determine price")
		return
	}

	// Build delegation request
	delegReq := deleg.DelegationRequest{
		PartialTxHex:   proof.PartialTxHex,
		ChallengeHash:  proof.ChallengeHash,
		ExpectedPayee:  cfg.PayeeAddress,
		ExpectedAmount: amount,
		NonceTxID:      nonceUTXO.TxID,
		NonceVout:      nonceUTXO.Vout,
		NonceScript:    nonceUTXO.Script,
		NonceSatoshis:  nonceUTXO.Satoshis,
	}

	// Delegate the transaction
	result, err := cfg.Delegator.Accept(delegReq)
	if err != nil {
		if delegErr, ok := err.(*deleg.DelegationError); ok {
			logger.Warn("delegation rejected",
				"code", delegErr.Code,
				"message", delegErr.Message,
			)
			writeError(w, delegErr.Status, delegErr.Code, delegErr.Message)
			return
		}
		logger.Error("delegation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "DELEGATION_ERROR", err.Error())
		return
	}

	// Success — add receipt header and pass through to the protected handler
	receiptHash := computeReceiptHash(result.TxID, proof.ChallengeHash)
	w.Header().Set(ReceiptHeader, receiptHash)
	w.Header().Set("X-402-TxID", result.TxID)

	logger.Info("payment accepted",
		"txid", result.TxID,
		"path", r.URL.Path,
		"receipt", receiptHash,
	)

	next.ServeHTTP(w, r)
}

// extractNonceOutpoint parses a partial transaction hex and returns the first input's outpoint.
// Uses the go-sdk transaction parser.
func extractNonceOutpoint(txHex string) (string, uint32, error) {
	tx, err := transaction.NewTransactionFromHex(txHex)
	if err != nil {
		return "", 0, err
	}
	if tx.InputCount() < 1 {
		return "", 0, &deleg.DelegationError{
			Code:    "INVALID_TRANSACTION",
			Message: "partial tx has no inputs",
			Status:  402,
		}
	}
	input0 := tx.Inputs[0]
	txid := hex.EncodeToString(input0.SourceTXID[:])
	return txid, input0.SourceTxOutIndex, nil
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
