// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package challenge

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// vectorFile is the relative path from the repo root. Tests run from the
// package directory, so we walk up two levels.
const vectorFile = "../../testdata/x402-vectors-v1.json"

type vectorSet struct {
	Version string   `json:"version"`
	Vectors []vector `json:"vectors"`
}

type vector struct {
	Name                   string `json:"name"`
	ExpectedResult         string `json:"expected_result"`
	CanonicalChallengeJSON string `json:"canonical_challenge_json"`
	CanonicalChallengeHex  string `json:"canonical_challenge_hex"`
	ChallengeSHA256        string `json:"challenge_sha256"`
	ChallengeBase64URL     string `json:"challenge_base64url"`
	HeaderBindingString    string `json:"header_binding_string"`
	HeaderBindingHex       string `json:"header_binding_hex"`
	HeadersSHA256          string `json:"headers_sha256"`
	BodySHA256             string `json:"body_sha256"`
	BodyBytes              string `json:"body_bytes"`
	RawTxHex               string `json:"rawtx_hex"`
	TxID                   string `json:"txid"`
}

func loadVectors(t *testing.T) vectorSet {
	t.Helper()
	data, err := os.ReadFile(vectorFile)
	if err != nil {
		t.Skipf("vector file not found: %v (run 'go run ./cmd/vecgen > testdata/x402-vectors-v1.json')", err)
	}
	var vs vectorSet
	if err := json.Unmarshal(data, &vs); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return vs
}

// TestVectors_ChallengeHash verifies that SHA256(canonical_challenge_json) matches
// the recorded challenge_sha256 for each vector that has both fields.
func TestVectors_ChallengeHash(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.CanonicalChallengeJSON == "" || v.ChallengeSHA256 == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			h := sha256.Sum256([]byte(v.CanonicalChallengeJSON))
			got := hex.EncodeToString(h[:])
			if got != v.ChallengeSHA256 {
				t.Errorf("challenge_sha256 mismatch:\n  canonical: %s\n  got:  %s\n  want: %s",
					v.CanonicalChallengeJSON, got, v.ChallengeSHA256)
			}
		})
	}
}

// TestVectors_ChallengeHex verifies that the hex encoding of canonical JSON bytes
// matches the recorded canonical_challenge_hex.
func TestVectors_ChallengeHex(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.CanonicalChallengeJSON == "" || v.CanonicalChallengeHex == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			got := hex.EncodeToString([]byte(v.CanonicalChallengeJSON))
			if got != v.CanonicalChallengeHex {
				t.Errorf("canonical_challenge_hex mismatch:\n  got:  %s\n  want: %s", got, v.CanonicalChallengeHex)
			}
		})
	}
}

// TestVectors_Base64URL verifies that base64url(canonical_json) matches the
// recorded challenge_base64url.
func TestVectors_Base64URL(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.CanonicalChallengeJSON == "" || v.ChallengeBase64URL == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			got := base64.RawURLEncoding.EncodeToString([]byte(v.CanonicalChallengeJSON))
			if got != v.ChallengeBase64URL {
				t.Errorf("base64url mismatch:\n  got:  %s\n  want: %s", got, v.ChallengeBase64URL)
			}
		})
	}
}

// TestVectors_HeaderHash verifies that SHA256(header_binding_string) matches
// the recorded headers_sha256.
func TestVectors_HeaderHash(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.HeaderBindingString == "" || v.HeadersSHA256 == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			h := sha256.Sum256([]byte(v.HeaderBindingString))
			got := hex.EncodeToString(h[:])
			if got != v.HeadersSHA256 {
				t.Errorf("headers_sha256 mismatch:\n  input: %q\n  got:  %s\n  want: %s",
					v.HeaderBindingString, got, v.HeadersSHA256)
			}
		})
	}
}

// TestVectors_BodyHash verifies SHA256(body_bytes) matches body_sha256.
func TestVectors_BodyHash(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.BodyBytes == "" || v.BodySHA256 == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			bodyBytes, err := hex.DecodeString(v.BodyBytes)
			if err != nil {
				t.Fatalf("hex decode body: %v", err)
			}
			h := sha256.Sum256(bodyBytes)
			got := hex.EncodeToString(h[:])
			if got != v.BodySHA256 {
				t.Errorf("body_sha256 mismatch:\n  got:  %s\n  want: %s", got, v.BodySHA256)
			}
		})
	}
}

// TestVectors_TxID verifies txid = SHA256(SHA256(rawtx)) byte-reversed.
func TestVectors_TxID(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.RawTxHex == "" || v.TxID == "" {
			continue
		}
		// Skip vectors where TxID is a compound description (e.g., "correct=..., submitted=...")
		if len(v.TxID) != 64 {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			rawtx, err := hex.DecodeString(v.RawTxHex)
			if err != nil {
				t.Fatalf("hex decode rawtx: %v", err)
			}
			h1 := sha256.Sum256(rawtx)
			h2 := sha256.Sum256(h1[:])
			// Bitcoin txid is byte-reversed
			var reversed [32]byte
			for i := 0; i < 32; i++ {
				reversed[i] = h2[31-i]
			}
			got := hex.EncodeToString(reversed[:])
			if got != v.TxID {
				t.Errorf("txid mismatch:\n  got:  %s\n  want: %s", got, v.TxID)
			}
		})
	}
}

// TestVectors_CanonicalJSONReproducibility verifies that CanonicalJSON produces
// the same bytes as recorded in the vectors by parsing the challenge JSON back
// into a map and re-canonicalizing.
func TestVectors_CanonicalJSONReproducibility(t *testing.T) {
	vs := loadVectors(t)

	for _, v := range vs.Vectors {
		if v.CanonicalChallengeJSON == "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			// Parse the canonical JSON into a generic map.
			var generic any
			if err := json.Unmarshal([]byte(v.CanonicalChallengeJSON), &generic); err != nil {
				t.Fatalf("unmarshal canonical: %v", err)
			}
			// Re-canonicalize.
			recanonical, err := CanonicalJSON(generic)
			if err != nil {
				t.Fatalf("CanonicalJSON: %v", err)
			}
			if string(recanonical) != v.CanonicalChallengeJSON {
				t.Errorf("re-canonicalization mismatch:\n  original: %s\n  re-canon: %s",
					v.CanonicalChallengeJSON, string(recanonical))
			}
		})
	}
}
