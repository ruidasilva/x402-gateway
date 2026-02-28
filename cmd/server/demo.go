package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/merkle-works/x402-gateway/internal/challenge"
	"github.com/merkle-works/x402-gateway/internal/config"
	"github.com/merkle-works/x402-gateway/internal/delegator"
	"github.com/merkle-works/x402-gateway/internal/gatekeeper"
	"github.com/merkle-works/x402-gateway/internal/pool"
)

// seedDemoPools populates nonce, fee, and payment pools with synthetic UTXOs so
// the full 402 → proof → 200 flow works locally without any blockchain.
// Three pools:
//   - Nonce pool: 1-sat UTXOs for replay protection
//   - Fee pool: 1-sat UTXOs for miner fees
//   - Payment pool: 100-sat UTXOs for service payments
//
// Works with both MemoryPool and RedisPool (uses Pool interface).
func seedDemoPools(noncePool, feePool, paymentPool pool.Pool, count int, feeRate float64, logger *slog.Logger) {
	// Seed nonce pool: N × 1-sat UTXOs for replay protection
	nonceScript, err := noncePool.LockingScriptHex()
	if err != nil {
		logger.Error("demo seed: failed to get nonce locking script", "error", err)
		return
	}

	nonceUTXOs := make([]pool.UTXO, count)
	for i := 0; i < count; i++ {
		nonceUTXOs[i] = pool.UTXO{
			TxID:     syntheticTxID(i),
			Vout:     0,
			Script:   nonceScript,
			Satoshis: 1,
		}
	}
	noncePool.AddExisting(nonceUTXOs)

	// Seed fee pool: N × 1-sat UTXOs
	feeScript, err := feePool.LockingScriptHex()
	if err != nil {
		logger.Error("demo seed: failed to get fee locking script", "error", err)
		return
	}

	feeUTXOs := make([]pool.UTXO, count)
	for i := 0; i < count; i++ {
		feeUTXOs[i] = pool.UTXO{
			TxID:     syntheticTxID(count + i), // offset to avoid txid collision
			Vout:     0,
			Script:   feeScript,
			Satoshis: 1,
		}
	}
	feePool.AddExisting(feeUTXOs)

	// Seed payment pool: N × 100-sat UTXOs for service payments
	paymentScript, err := paymentPool.LockingScriptHex()
	if err != nil {
		logger.Error("demo seed: failed to get payment locking script", "error", err)
		return
	}

	paymentUTXOs := make([]pool.UTXO, count)
	for i := 0; i < count; i++ {
		paymentUTXOs[i] = pool.UTXO{
			TxID:     syntheticTxID(2*count + i), // offset to avoid txid collision
			Vout:     0,
			Script:   paymentScript,
			Satoshis: 100,
		}
	}
	paymentPool.AddExisting(paymentUTXOs)

	logger.Info("demo mode: pools seeded",
		"nonce_utxos", count,
		"fee_utxos", count,
		"payment_utxos", count,
		"nonce_sats", 1,
		"fee_sats", 1,
		"payment_sats", 100,
		"fee_rate", feeRate,
	)
}

// syntheticTxID generates a unique 64-char hex txid.
// First 4 bytes encode the index for debuggability; remaining 28 bytes are random.
func syntheticTxID(index int) string {
	b := make([]byte, 32)
	rand.Read(b) //nolint:errcheck // crypto/rand never fails on supported platforms
	b[0] = byte(index >> 24)
	b[1] = byte(index >> 16)
	b[2] = byte(index >> 8)
	b[3] = byte(index)
	return hex.EncodeToString(b)
}

// ──── HTTP Handlers ────

// buildProofRequest is the JSON body for POST /demo/build-proof.
type buildProofRequest struct {
	ChallengeEncoded string `json:"challenge"` // base64url-encoded challenge from X402-Challenge
}

// buildProofResponse is the JSON response from POST /demo/build-proof.
type buildProofResponse struct {
	ProofHeader   string `json:"proof_header"`   // ready to use as X402-Proof value
	TxID          string `json:"txid"`           // for display
	ChallengeHash string `json:"challenge_hash"` // for display
}

// handleBuildProof builds a complete proof server-side so the dashboard
// doesn't need BSV crypto. This endpoint:
//  1. Decodes the challenge
//  2. Calls the delegator to build the full transaction, sign, and broadcast
//  3. Builds a spec-compliant proof with the completed transaction
func handleBuildProof(key *ec.PrivateKey, mainnet bool, deleg *delegator.Delegator, payeeLockingScriptHex string) http.HandlerFunc {
	_ = key     // reserved for future client-signing support
	_ = mainnet // reserved for future address derivation
	return func(w http.ResponseWriter, r *http.Request) {
		var req buildProofRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "invalid request body: " + err.Error(),
			})
			return
		}

		// 1. Decode the challenge
		ch, err := challenge.Decode(req.ChallengeEncoded)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "decode challenge: " + err.Error(),
			})
			return
		}

		// 2. Compute challenge hash
		challengeHash, err := challenge.ComputeHash(ch)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "compute challenge hash: " + err.Error(),
			})
			return
		}

		// 3. Call delegator to build full transaction, sign, and broadcast
		delegReq := delegator.DelegationRequest{
			ChallengeHash:                 challengeHash,
			ExpectedPayeeLockingScriptHex: payeeLockingScriptHex,
			ExpectedAmount:                ch.AmountSats,
			NonceUTXO:                     ch.NonceUTXO,
		}

		delegResult, err := deleg.Accept(delegReq)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "delegation failed: " + err.Error(),
			})
			return
		}

		// 4. Build spec-compliant proof
		rawTxBytes, err := hex.DecodeString(delegResult.RawTxHex)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "decode rawtx: " + err.Error(),
			})
			return
		}
		rawTxB64 := base64.StdEncoding.EncodeToString(rawTxBytes)

		// Compute body and headers hash (empty for GET from dashboard)
		bodyHash := challenge.HashBody(nil)
		emptyHeaders := make(http.Header)
		headersHash := challenge.HashHeaders(emptyHeaders, gatekeeper.HeaderAllowlist)

		proof := &gatekeeper.Proof{
			V:               challenge.Version,
			Scheme:          challenge.Scheme,
			TxID:            delegResult.TxID,
			RawTxB64:        rawTxB64,
			ChallengeSHA256: challengeHash,
			Request: gatekeeper.RequestBinding{
				Domain:           ch.Domain,
				Method:           ch.Method,
				Path:             ch.Path,
				Query:            ch.Query,
				ReqHeadersSHA256: headersHash,
				ReqBodySHA256:    bodyHash,
			},
		}
		proofEncoded, err := gatekeeper.EncodeProof(proof)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "encode proof: " + err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, buildProofResponse{
			ProofHeader:   proofEncoded,
			TxID:          delegResult.TxID,
			ChallengeHash: challengeHash,
		})
	}
}

// handleDemoInfo returns server state for the dashboard.
func handleDemoInfo(cfg *config.Config, noncePool, feePool, paymentPool pool.Pool, payeeAddr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"version":      "1.0.0",
			"mode":         "demo",
			"network":      cfg.BSVNetwork,
			"payee":        payeeAddr,
			"port":         cfg.Port,
			"nonce_pool":   noncePool.Stats(),
			"fee_pool":     feePool.Stats(),
			"payment_pool": paymentPool.Stats(),
			"price_sats":   100,
		})
	}
}

// writeJSON encodes a value as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
