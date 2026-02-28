package challenge

// Challenge is the 402 payment challenge sent to the client.
type Challenge struct {
	V                     string `json:"v"`                        // "1"
	Scheme                string `json:"scheme"`                   // "bsv-tx-v1"
	AmountSats            int64  `json:"amount_sats"`              // price in satoshis
	PayeeLockingScriptHex string `json:"payee_locking_script_hex"` // hex-encoded locking script
	ExpiresAt             int64  `json:"expires_at"`               // unix timestamp (seconds)

	// Request binding fields (flat, per spec)
	Domain           string `json:"domain"`             // host header
	Method           string `json:"method"`             // HTTP method
	Path             string `json:"path"`               // request path
	Query            string `json:"query"`              // raw query string (empty string if none)
	ReqHeadersSHA256 string `json:"req_headers_sha256"` // SHA-256 of canonical headers
	ReqBodySHA256    string `json:"req_body_sha256"`    // SHA-256 of body bytes

	// Settlement parameters (spec-defined)
	RequireMempoolAccept  bool `json:"require_mempool_accept"`  // true = 0-conf gating
	ConfirmationsRequired int  `json:"confirmations_required"`  // 0 for instant
}
