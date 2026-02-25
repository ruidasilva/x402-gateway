package delegator

// DelegationRequest is the input to the delegator's Accept method.
// The client calls the delegator directly with a partial transaction.
type DelegationRequest struct {
	// PartialTxHex is the hex-encoded partial transaction from the client.
	// Must have: input 0 = nonce UTXO (unsigned), output 0 = payee.
	PartialTxHex string `json:"partial_tx"`

	// ChallengeHash is the SHA-256 of the original challenge (for reference).
	ChallengeHash string `json:"challenge_hash"`

	// NonceTxID and NonceVout identify the nonce UTXO from the challenge.
	NonceTxID string `json:"nonce_txid"`
	NonceVout uint32 `json:"nonce_vout"`

	// --- Server-side enrichment (not in client JSON) ---

	// ExpectedPayeeLockingScriptHex is the expected payee locking script.
	ExpectedPayeeLockingScriptHex string `json:"-"`

	// ExpectedAmount is the minimum expected payment amount.
	ExpectedAmount int64 `json:"-"`

	// NonceLockingScriptHex is the hex-encoded locking script of the nonce UTXO.
	NonceLockingScriptHex string `json:"-"`

	// NonceSatoshis is the value of the nonce UTXO (always 1).
	NonceSatoshis int64 `json:"-"`
}

// DelegationResult is returned after successful delegation.
// The client uses txid + rawtx to build the proof for the gatekeeper.
type DelegationResult struct {
	TxID     string `json:"txid"`
	RawTxHex string `json:"rawtx_hex"`
	Accepted bool   `json:"accepted"`
}

// DelegationError represents a specific delegation failure.
// Uses spec error codes from 04-Protocol-Spec.md.
type DelegationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"` // HTTP status code
}

func (e *DelegationError) Error() string {
	return e.Code + ": " + e.Message
}

// Standard delegation error codes with correct HTTP status mappings.
// Error code strings match 04-Protocol-Spec.md.
var (
	// 400 — malformed request
	ErrInvalidProof       = &DelegationError{Code: "invalid_proof", Message: "invalid partial transaction", Status: 400}
	ErrUnexpectedOutputs  = &DelegationError{Code: "invalid_proof", Message: "partial tx must have exactly 1 output (payee)", Status: 400}
	ErrNonceAlreadySigned = &DelegationError{Code: "invalid_proof", Message: "nonce input must be unsigned", Status: 400}

	// 400 — nonce problems
	ErrInvalidNonce = &DelegationError{Code: "invalid_nonce", Message: "input 0 does not reference the expected nonce UTXO", Status: 400}

	// 402 — payment problems (client must fix and retry)
	ErrInsufficientAmount = &DelegationError{Code: "insufficient_amount", Message: "output 0 amount is below minimum", Status: 402}
	ErrExpiredChallenge   = &DelegationError{Code: "expired_challenge", Message: "challenge has expired", Status: 402}

	// 403 — payee/binding mismatch
	ErrInvalidPayee   = &DelegationError{Code: "invalid_payee", Message: "output 0 does not pay the expected address", Status: 403}
	ErrInvalidBinding = &DelegationError{Code: "invalid_binding", Message: "request does not match challenge binding", Status: 403}
	ErrInvalidVersion = &DelegationError{Code: "invalid_version", Message: "challenge version does not match expected", Status: 400}
	ErrInvalidScheme  = &DelegationError{Code: "invalid_scheme", Message: "challenge scheme does not match expected", Status: 400}

	// 409 — replay / double-spend
	ErrDoubleSpend = &DelegationError{Code: "double_spend", Message: "nonce UTXO has already been spent", Status: 409}

	// 500 — server errors
	ErrMempoolRejected = &DelegationError{Code: "mempool_rejected", Message: "transaction broadcast failed", Status: 500}

	// 503 — resource exhaustion
	ErrNoFeeUTXO = &DelegationError{Code: "no_nonces_available", Message: "no fee UTXOs available", Status: 503}
)
