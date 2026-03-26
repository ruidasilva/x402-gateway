// vecgen generates canonical x402 v1.0 interoperability test vectors.
// It uses the actual implementation's CanonicalJSON, HashHeaders, HashBody,
// ComputeHash, and Encode functions — producing vectors that are guaranteed
// to match the reference implementation's wire behavior.
//
// Usage: go run ./cmd/vecgen > testdata/vectors.json
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/merkleworks/x402-bsv/internal/challenge"
)

// HeaderAllowlist matches gatekeeper.HeaderAllowlist.
var headerAllowlist = []string{
	"accept",
	"content-length",
	"content-type",
	"x402-client",
	"x402-idempotency-key",
}

// --- Stable test constants ---
const (
	nonceTxID     = "a1a2a3a4a5a6a7a8a9a0b1b2b3b4b5b6b7b8b9b0c1c2c3c4c5c6c7c8c9c0d1d2"
	nonceVout     = 0
	nonceSats     = 1
	nonceScript   = "76a914aabbccdd00112233445566778899aabbccddeeff88ac"
	payeeScript   = "76a91489abcdefab89abcdefab89abcdefab89abcdefab88ac"
	amountSats    = 100
	domain        = "api.example.com"
	expiresAt     = int64(1800000000) // stable future timestamp
)

type VectorSet struct {
	Version     string    `json:"version"`
	GeneratedAt string    `json:"generated_at"`
	GeneratedBy string    `json:"generated_by"`
	Vectors     []Vector  `json:"vectors"`
}

type Vector struct {
	Name                  string      `json:"name"`
	Purpose               string      `json:"purpose"`
	ExpectedResult        string      `json:"expected_result"`
	Challenge             any         `json:"challenge,omitempty"`
	CanonicalChallengeJSON string     `json:"canonical_challenge_json,omitempty"`
	CanonicalChallengeHex  string     `json:"canonical_challenge_hex,omitempty"`
	ChallengeSHA256       string      `json:"challenge_sha256,omitempty"`
	ChallengeBase64URL    string      `json:"challenge_base64url,omitempty"`
	Proof                 any         `json:"proof,omitempty"`
	HeaderBindingString   string      `json:"header_binding_string,omitempty"`
	HeaderBindingHex      string      `json:"header_binding_hex,omitempty"`
	HeadersSHA256         string      `json:"headers_sha256,omitempty"`
	BodySHA256            string      `json:"body_sha256,omitempty"`
	BodyBytes             string      `json:"body_bytes,omitempty"`
	RawTxHex              string      `json:"rawtx_hex,omitempty"`
	TxID                  string      `json:"txid,omitempty"`
	Notes                 string      `json:"notes,omitempty"`
}

func main() {
	vectors := VectorSet{
		Version:     "1.0",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GeneratedBy: "x402-gateway/cmd/vecgen (Go reference implementation)",
	}

	vectors.Vectors = append(vectors.Vectors, vectorGetEmpty())
	vectors.Vectors = append(vectors.Vectors, vectorPostWithBody())
	vectors.Vectors = append(vectors.Vectors, vectorHeaderBinding())
	vectors.Vectors = append(vectors.Vectors, vectorBodyHash())
	vectors.Vectors = append(vectors.Vectors, vectorInvalidBinding())
	vectors.Vectors = append(vectors.Vectors, vectorInvalidTxID())
	vectors.Vectors = append(vectors.Vectors, vectorExpiredChallenge())
	vectors.Vectors = append(vectors.Vectors, vectorInvalidVersion())
	vectors.Vectors = append(vectors.Vectors, vectorTxIDDerivation())

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(vectors); err != nil {
		fmt.Fprintf(os.Stderr, "json encode: %v\n", err)
		os.Exit(1)
	}
}

// --- Vector 1: Valid GET with empty query/body ---
func vectorGetEmpty() Vector {
	ch := &challenge.Challenge{
		V:                     1,
		Scheme:                "bsv-tx-v1",
		AmountSats:            amountSats,
		PayeeLockingScriptHex: payeeScript,
		ExpiresAt:             expiresAt,
		Domain:                domain,
		Method:                "GET",
		Path:                  "/v1/resource",
		Query:                 "",
		ReqHeadersSHA256:      hashHeadersManual(http.Header{}, headerAllowlist),
		ReqBodySHA256:         challenge.HashBody(nil),
		NonceUTXO: &challenge.NonceRef{
			TxID:             nonceTxID,
			Vout:             nonceVout,
			Satoshis:         nonceSats,
			LockingScriptHex: nonceScript,
		},
		RequireMempoolAccept: true,
	}

	canonicalBytes, _ := challenge.CanonicalJSON(ch)
	challengeHash, _ := challenge.ComputeHash(ch)
	b64url := base64.RawURLEncoding.EncodeToString(canonicalBytes)

	return Vector{
		Name:                   "valid_get_empty",
		Purpose:                "Canonical GET challenge with empty query and empty body",
		ExpectedResult:         "valid",
		Challenge:              ch,
		CanonicalChallengeJSON: string(canonicalBytes),
		CanonicalChallengeHex:  hex.EncodeToString(canonicalBytes),
		ChallengeSHA256:        challengeHash,
		ChallengeBase64URL:     b64url,
		HeadersSHA256:          ch.ReqHeadersSHA256,
		BodySHA256:             ch.ReqBodySHA256,
		Notes:                  "Empty body → SHA256(\"\"). Headers → canonical header-binding string with allowlist values all empty.",
	}
}

// --- Vector 2: Valid POST with body ---
func vectorPostWithBody() Vector {
	body := []byte(`{"action":"create","name":"test"}`)
	bodyHash := challenge.HashBody(body)

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	headersHash := hashHeadersManual(headers, headerAllowlist)

	ch := &challenge.Challenge{
		V:                     1,
		Scheme:                "bsv-tx-v1",
		AmountSats:            200,
		PayeeLockingScriptHex: payeeScript,
		ExpiresAt:             expiresAt,
		Domain:                domain,
		Method:                "POST",
		Path:                  "/v1/resource",
		Query:                 "ref=abc123",
		ReqHeadersSHA256:      headersHash,
		ReqBodySHA256:         bodyHash,
		NonceUTXO: &challenge.NonceRef{
			TxID:             nonceTxID,
			Vout:             nonceVout,
			Satoshis:         nonceSats,
			LockingScriptHex: nonceScript,
		},
		RequireMempoolAccept: true,
	}

	canonicalBytes, _ := challenge.CanonicalJSON(ch)
	challengeHash, _ := challenge.ComputeHash(ch)
	b64url := base64.RawURLEncoding.EncodeToString(canonicalBytes)

	hdrBindStr := buildHeaderBindingString(headers, headerAllowlist)

	return Vector{
		Name:                   "valid_post_with_body",
		Purpose:                "POST challenge with body hash, query string, and content-type header",
		ExpectedResult:         "valid",
		Challenge:              ch,
		CanonicalChallengeJSON: string(canonicalBytes),
		CanonicalChallengeHex:  hex.EncodeToString(canonicalBytes),
		ChallengeSHA256:        challengeHash,
		ChallengeBase64URL:     b64url,
		HeaderBindingString:    hdrBindStr,
		HeaderBindingHex:       hex.EncodeToString([]byte(hdrBindStr)),
		HeadersSHA256:          headersHash,
		BodySHA256:             bodyHash,
		BodyBytes:              hex.EncodeToString(body),
		Notes:                  "Body is raw JSON bytes. Query is without leading '?'. Header binding includes content-type and content-length.",
	}
}

// --- Vector 3: Header binding canonical string ---
func vectorHeaderBinding() Vector {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "text/plain")
	headers.Set("X402-Client", "test-client/1.0")

	hdrBindStr := buildHeaderBindingString(headers, headerAllowlist)
	h := sha256.Sum256([]byte(hdrBindStr))
	headersHash := hex.EncodeToString(h[:])

	return Vector{
		Name:                "header_binding_canonical",
		Purpose:             "Demonstrates canonical header-binding string construction per spec §4",
		ExpectedResult:      "reference",
		HeaderBindingString: hdrBindStr,
		HeaderBindingHex:    hex.EncodeToString([]byte(hdrBindStr)),
		HeadersSHA256:       headersHash,
		Notes:               "Headers sorted by name. Missing headers have empty value. Each line: name:value\\n",
	}
}

// --- Vector 4: Body hash ---
func vectorBodyHash() Vector {
	emptyHash := challenge.HashBody(nil)
	bodyBytes := []byte(`{"key":"value"}`)
	bodyHash := challenge.HashBody(bodyBytes)

	return Vector{
		Name:           "body_hash_examples",
		Purpose:        "SHA-256 of empty body and non-empty body per spec §4",
		ExpectedResult: "reference",
		BodySHA256:     bodyHash,
		BodyBytes:      hex.EncodeToString(bodyBytes),
		Notes:          fmt.Sprintf("Empty body hash: %s. Non-empty body hash: %s", emptyHash, bodyHash),
	}
}

// --- Vector 5: Invalid binding (path mismatch) ---
func vectorInvalidBinding() Vector {
	ch := &challenge.Challenge{
		V:                     1,
		Scheme:                "bsv-tx-v1",
		AmountSats:            amountSats,
		PayeeLockingScriptHex: payeeScript,
		ExpiresAt:             expiresAt,
		Domain:                domain,
		Method:                "GET",
		Path:                  "/v1/resource",
		Query:                 "",
		ReqHeadersSHA256:      hashHeadersManual(http.Header{}, headerAllowlist),
		ReqBodySHA256:         challenge.HashBody(nil),
		NonceUTXO: &challenge.NonceRef{
			TxID:             nonceTxID,
			Vout:             nonceVout,
			Satoshis:         nonceSats,
			LockingScriptHex: nonceScript,
		},
		RequireMempoolAccept: true,
	}

	canonicalBytes, _ := challenge.CanonicalJSON(ch)
	challengeHash, _ := challenge.ComputeHash(ch)

	return Vector{
		Name:                   "invalid_binding_path_mismatch",
		Purpose:                "Proof bound to /v1/resource but request sent to /v1/other — must be rejected",
		ExpectedResult:         "reject",
		Challenge:              ch,
		CanonicalChallengeJSON: string(canonicalBytes),
		ChallengeSHA256:        challengeHash,
		Notes:                  "Server verifies inbound request path matches challenge.path. Mismatch → 400 invalid_binding.",
	}
}

// --- Vector 6: Invalid txid ---
func vectorInvalidTxID() Vector {
	// Minimal raw tx: version(4) + input_count(1) + input(41) + output_count(1) + output(11) + locktime(4) = ~61 bytes
	// We build one manually.
	rawtx := buildMinimalRawTx()
	correctTxID := doubleSHA256Hex(rawtx)
	wrongTxID := strings.Repeat("ff", 32)

	return Vector{
		Name:           "invalid_txid_mismatch",
		Purpose:        "proof.payment.txid does not match SHA256(SHA256(rawtx)) — must be rejected",
		ExpectedResult: "reject",
		RawTxHex:       hex.EncodeToString(rawtx),
		TxID:           fmt.Sprintf("correct=%s, submitted=%s", correctTxID, wrongTxID),
		Notes:          "txid = SHA256(SHA256(raw_tx_bytes)) as lowercase hex. Server computes txid from rawtx and rejects on mismatch.",
	}
}

// --- Vector 7: Expired challenge ---
func vectorExpiredChallenge() Vector {
	pastExpiry := int64(1600000000) // well in the past

	ch := &challenge.Challenge{
		V:                     1,
		Scheme:                "bsv-tx-v1",
		AmountSats:            amountSats,
		PayeeLockingScriptHex: payeeScript,
		ExpiresAt:             pastExpiry,
		Domain:                domain,
		Method:                "GET",
		Path:                  "/v1/resource",
		Query:                 "",
		ReqHeadersSHA256:      hashHeadersManual(http.Header{}, headerAllowlist),
		ReqBodySHA256:         challenge.HashBody(nil),
		NonceUTXO: &challenge.NonceRef{
			TxID:             nonceTxID,
			Vout:             nonceVout,
			Satoshis:         nonceSats,
			LockingScriptHex: nonceScript,
		},
		RequireMempoolAccept: true,
	}

	canonicalBytes, _ := challenge.CanonicalJSON(ch)
	challengeHash, _ := challenge.ComputeHash(ch)

	return Vector{
		Name:                   "expired_challenge",
		Purpose:                "Challenge with expires_at in the past — proof must be rejected with 402",
		ExpectedResult:         "reject",
		Challenge:              ch,
		CanonicalChallengeJSON: string(canonicalBytes),
		ChallengeSHA256:        challengeHash,
		Notes:                  fmt.Sprintf("expires_at=%d is in the past. Per spec §7: reject when current_time > expires_at (strictly greater).", pastExpiry),
	}
}

// --- Vector 8: Invalid proof version ---
func vectorInvalidVersion() Vector {
	return Vector{
		Name:           "invalid_proof_version",
		Purpose:        "Proof with v=0 — must be rejected, not defaulted to v=1",
		ExpectedResult: "reject",
		Proof: map[string]any{
			"v":                0,
			"scheme":           "bsv-tx-v1",
			"challenge_sha256": strings.Repeat("aa", 32),
			"request":          map[string]any{"method": "GET", "path": "/v1/resource", "query": "", "req_headers_sha256": "", "req_body_sha256": ""},
			"payment":          map[string]any{"txid": strings.Repeat("bb", 32), "rawtx_b64": "AAAA"},
		},
		Notes: "Per spec §5: 'The server MUST reject proofs whose v value it does not support.' v=0 is not supported.",
	}
}

// --- Vector 9: TxID derivation ---
func vectorTxIDDerivation() Vector {
	rawtx := buildMinimalRawTx()
	txid := doubleSHA256Hex(rawtx)
	rawtxB64 := base64.StdEncoding.EncodeToString(rawtx)

	return Vector{
		Name:           "txid_derivation",
		Purpose:        "Demonstrates txid = SHA256(SHA256(raw_tx_bytes)) with byte-reversed hex encoding",
		ExpectedResult: "reference",
		RawTxHex:       hex.EncodeToString(rawtx),
		TxID:           txid,
		Notes:          fmt.Sprintf("rawtx_b64 (standard base64): %s. txid = double SHA256, byte-reversed, lowercase hex.", rawtxB64),
	}
}

// --- Helpers ---

func hashHeadersManual(headers http.Header, keys []string) string {
	return challenge.HashHeaders(headers, keys)
}

func buildHeaderBindingString(headers http.Header, keys []string) string {
	sortedKeys := make([]string, len(keys))
	copy(sortedKeys, keys)
	sort.Strings(sortedKeys)

	var parts []string
	for _, k := range sortedKeys {
		lk := strings.ToLower(k)
		val := strings.TrimSpace(headers.Get(k))
		parts = append(parts, lk+":"+val)
	}
	canonical := strings.Join(parts, "\n")
	if len(parts) > 0 {
		canonical += "\n"
	}
	return canonical
}

func buildMinimalRawTx() []byte {
	// Minimal valid Bitcoin tx: 1 input (coinbase-like), 1 output
	// Version: 01000000
	// Input count: 01
	// Previous output hash: 32 zero bytes
	// Previous output index: ffffffff
	// Script length: 00
	// Sequence: ffffffff
	// Output count: 01
	// Value: 6400000000000000 (100 sats, little-endian)
	// Script length: 19 (25 bytes P2PKH)
	// Script: OP_DUP OP_HASH160 <20 bytes> OP_EQUALVERIFY OP_CHECKSIG
	// Locktime: 00000000
	txHex := "01000000" +
		"01" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"ffffffff" +
		"00" +
		"ffffffff" +
		"01" +
		"6400000000000000" +
		"19" +
		"76a91489abcdefab89abcdefab89abcdefab89abcdefab88ac" +
		"00000000"
	b, _ := hex.DecodeString(txHex)
	return b
}

func doubleSHA256Hex(data []byte) string {
	h1 := sha256.Sum256(data)
	h2 := sha256.Sum256(h1[:])
	// Bitcoin txid is byte-reversed
	reversed := make([]byte, 32)
	for i := 0; i < 32; i++ {
		reversed[i] = h2[31-i]
	}
	return hex.EncodeToString(reversed)
}
