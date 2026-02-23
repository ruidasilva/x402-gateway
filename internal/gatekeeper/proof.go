package gatekeeper

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ParseProof decodes the X-402-Proof header value into a Proof struct.
// The header value is base64url-encoded JSON.
func ParseProof(header string) (*Proof, error) {
	if header == "" {
		return nil, fmt.Errorf("empty proof header")
	}

	data, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		return nil, fmt.Errorf("base64url decode: %w", err)
	}

	var p Proof
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	if p.PartialTxHex == "" {
		return nil, fmt.Errorf("missing partial_tx field")
	}
	if p.ChallengeHash == "" {
		return nil, fmt.Errorf("missing challenge_hash field")
	}

	return &p, nil
}

// EncodeProof encodes a Proof struct to base64url JSON for use in the X-402-Proof header.
func EncodeProof(p *Proof) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
