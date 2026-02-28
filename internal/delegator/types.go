package delegator

// DelegationRequest is the input to the delegator's Accept method.
// The delegator builds the full transaction internally.
type DelegationRequest struct {
	// ChallengeHash is the SHA-256 of the original challenge.
	ChallengeHash string `json:"challenge_hash"`

	// ExpectedPayeeLockingScriptHex is the expected payee locking script.
	ExpectedPayeeLockingScriptHex string `json:"payee_locking_script_hex,omitempty"`

	// ExpectedAmount is the minimum expected payment amount.
	ExpectedAmount int64 `json:"amount_sats,omitempty"`
}

// DelegationResult is returned after successful delegation.
// The client uses txid + rawtx to build the proof for the gatekeeper.
type DelegationResult struct {
	TxID     string `json:"txid"`
	RawTxHex string `json:"rawtx_hex"`
	Accepted bool   `json:"accepted"`
}

// DelegationError represents a specific delegation failure.
type DelegationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"` // HTTP status code
}

func (e *DelegationError) Error() string {
	return e.Code + ": " + e.Message
}

// Standard delegation error codes.
var (
	// 400 — malformed request
	ErrInvalidProof      = &DelegationError{Code: "invalid_proof", Message: "invalid transaction", Status: 400}
	ErrUnexpectedOutputs = &DelegationError{Code: "invalid_proof", Message: "unexpected transaction outputs", Status: 400}

	// 402 — payment problems (client must fix and retry)
	ErrInsufficientAmount = &DelegationError{Code: "insufficient_amount", Message: "payment amount is below minimum", Status: 402}
	ErrExpiredChallenge   = &DelegationError{Code: "expired_challenge", Message: "challenge has expired", Status: 402}

	// 403 — payee/binding mismatch
	ErrInvalidPayee   = &DelegationError{Code: "invalid_payee", Message: "payment does not go to expected address", Status: 403}
	ErrInvalidBinding = &DelegationError{Code: "invalid_binding", Message: "request does not match challenge binding", Status: 403}
	ErrInvalidVersion = &DelegationError{Code: "invalid_version", Message: "challenge version does not match expected", Status: 400}
	ErrInvalidScheme  = &DelegationError{Code: "invalid_scheme", Message: "challenge scheme does not match expected", Status: 400}

	// 409 — replay / double-spend
	ErrDoubleSpend = &DelegationError{Code: "double_spend", Message: "challenge has already been settled", Status: 409}

	// 500 — server errors
	ErrMempoolRejected = &DelegationError{Code: "mempool_rejected", Message: "transaction broadcast failed", Status: 500}

	// 503 — resource exhaustion
	ErrNoUTXOAvailable = &DelegationError{Code: "no_utxos_available", Message: "no UTXOs available for delegation", Status: 503}
)
