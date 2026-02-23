package challenge

// Challenge is the 402 payment challenge returned to the client.
type Challenge struct {
	Scheme          string         `json:"scheme"`           // "bsv-tx-v1+delegated"
	Network         string         `json:"network"`          // "mainnet" or "testnet"
	Version         string         `json:"version"`          // "0.1"
	Nonce           NonceRef       `json:"nonce"`            // the leased nonce UTXO
	Payee           string         `json:"payee"`            // service BSV address
	Amount          uint64         `json:"amount"`           // price in satoshis
	Expiry          int64          `json:"expiry"`           // unix timestamp
	RequestBinding  RequestBinding `json:"request_binding"`  // canonical request hash
	ChallengeSHA256 string         `json:"challenge_sha256"` // SHA-256 of this challenge
}

// NonceRef identifies the nonce UTXO that the client must spend in the partial tx.
type NonceRef struct {
	TxID     string `json:"txid"`
	Vout     uint32 `json:"vout"`
	Script   string `json:"script"`   // hex-encoded locking script
	Satoshis uint64 `json:"satoshis"` // always 1
}

// RequestBinding binds the challenge to the specific HTTP request.
type RequestBinding struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	QueryHash   string `json:"query_hash"`   // SHA-256 of sorted query params
	BodyHash    string `json:"body_hash"`    // SHA-256 of body
	HeadersHash string `json:"headers_hash"` // SHA-256 of selected headers
	Domain      string `json:"domain"`
}
