package challenge

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/merkle-works/x402-gateway/internal/nonce"
)

const (
	// Scheme is the x402 payment scheme identifier.
	Scheme = "bsv-tx-v1+delegated"

	// Version is the protocol version.
	Version = "0.1"
)

// BuildOptions configures challenge generation.
type BuildOptions struct {
	PayeeAddress string
	Amount       uint64
	Network      string
	TTL          time.Duration
	BindHeaders  []string // which request headers to include in binding
}

// Build creates a 402 challenge from an HTTP request and a leased nonce UTXO.
func Build(req *http.Request, nonceUTXO *nonce.NonceUTXO, opts BuildOptions) (*Challenge, error) {
	// Read and restore the request body
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Build the request binding
	binding := RequestBinding{
		Method:      req.Method,
		Path:        req.URL.Path,
		QueryHash:   HashQuery(req.URL.Query()),
		BodyHash:    HashBody(bodyBytes),
		HeadersHash: HashHeaders(req.Header, opts.BindHeaders),
		Domain:      req.Host,
	}

	challenge := &Challenge{
		Scheme:  Scheme,
		Network: opts.Network,
		Version: Version,
		Nonce: NonceRef{
			TxID:     nonceUTXO.TxID,
			Vout:     nonceUTXO.Vout,
			Script:   nonceUTXO.Script,
			Satoshis: nonceUTXO.Satoshis,
		},
		Payee:          opts.PayeeAddress,
		Amount:         opts.Amount,
		Expiry:         time.Now().Add(opts.TTL).Unix(),
		RequestBinding: binding,
	}

	// Compute the challenge SHA-256 (hash of deterministic JSON, excluding the hash field itself)
	challengeHash, err := computeChallengeHash(challenge)
	if err != nil {
		return nil, fmt.Errorf("compute challenge hash: %w", err)
	}
	challenge.ChallengeSHA256 = challengeHash

	return challenge, nil
}

// computeChallengeHash produces a SHA-256 hex digest of the challenge JSON
// with the ChallengeSHA256 field set to empty (to avoid circular hashing).
func computeChallengeHash(c *Challenge) (string, error) {
	// Create a copy with empty hash field for deterministic hashing
	hashable := *c
	hashable.ChallengeSHA256 = ""

	data, err := json.Marshal(hashable)
	if err != nil {
		return "", err
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// Encode serializes a challenge to base64url for use in the WWW-Authenticate header.
func Encode(c *Challenge) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// Decode parses a base64url-encoded challenge.
func Decode(encoded string) (*Challenge, error) {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64url decode: %w", err)
	}

	var c Challenge
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	return &c, nil
}

// Verify checks that a challenge's SHA-256 is valid.
func Verify(c *Challenge) (bool, error) {
	expected, err := computeChallengeHash(c)
	if err != nil {
		return false, err
	}
	return expected == c.ChallengeSHA256, nil
}
