package delegator

// DelegationRequest is the input to the delegator's Accept method.
type DelegationRequest struct {
	// PartialTxHex is the hex-encoded partial transaction from the client.
	// Must have: input 0 = nonce UTXO, output 0 = payee.
	PartialTxHex string `json:"partial_tx"`

	// ChallengeHash is the SHA-256 of the original challenge.
	ChallengeHash string `json:"challenge_hash"`

	// ExpectedPayee is the expected payee address (from the challenge).
	ExpectedPayee string `json:"-"`

	// ExpectedAmount is the minimum expected payment amount (from the challenge).
	ExpectedAmount uint64 `json:"-"`

	// NonceTxID and NonceVout identify the expected nonce UTXO.
	NonceTxID string `json:"-"`
	NonceVout uint32 `json:"-"`

	// NonceScript is the hex-encoded locking script of the nonce UTXO.
	NonceScript string `json:"-"`

	// NonceSatoshis is the value of the nonce UTXO (always 1).
	NonceSatoshis uint64 `json:"-"`
}

// DelegationResult is returned after successful delegation.
type DelegationResult struct {
	TxID     string `json:"txid"`
	RawTx    string `json:"rawtx"`
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
	ErrInvalidTransaction = &DelegationError{Code: "INVALID_TRANSACTION", Message: "invalid partial transaction", Status: 402}
	ErrWrongNonce         = &DelegationError{Code: "WRONG_NONCE", Message: "input 0 does not reference the expected nonce UTXO", Status: 402}
	ErrWrongPayee         = &DelegationError{Code: "WRONG_PAYEE", Message: "output 0 does not pay the expected address", Status: 402}
	ErrInsufficientAmount = &DelegationError{Code: "INSUFFICIENT_AMOUNT", Message: "output 0 amount is below minimum", Status: 402}
	ErrReplayDetected     = &DelegationError{Code: "REPLAY_DETECTED", Message: "nonce UTXO has already been spent", Status: 409}
	ErrChallengeExpired   = &DelegationError{Code: "CHALLENGE_EXPIRED", Message: "challenge has expired", Status: 402}
	ErrBroadcastFailed    = &DelegationError{Code: "BROADCAST_FAILED", Message: "transaction broadcast failed", Status: 500}
	ErrNoFeeUTXO          = &DelegationError{Code: "NO_FEE_UTXO", Message: "no fee UTXOs available", Status: 503}
)
