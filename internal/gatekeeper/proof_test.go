// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0


package gatekeeper

import (
	"strings"
	"testing"
)

func TestParseAndEncodeProof(t *testing.T) {
	original := &Proof{
		V:               1,
		Scheme:          "bsv-tx-v1",
		ChallengeSHA256: "abc123def456",
		Payment: Payment{
			TxID:     strings.Repeat("a", 64),
			RawTxB64: "AQAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
		},
		Request: RequestBinding{
			Method: "GET",
			Path:   "/v1/expensive",
		},
	}

	encoded, err := EncodeProof(original)
	if err != nil {
		t.Fatalf("EncodeProof: %v", err)
	}
	if encoded == "" {
		t.Error("encoded should not be empty")
	}

	parsed, err := ParseProof(encoded)
	if err != nil {
		t.Fatalf("ParseProof: %v", err)
	}

	if parsed.V != original.V {
		t.Errorf("v mismatch: got %d, want %d", parsed.V, original.V)
	}
	if parsed.Scheme != original.Scheme {
		t.Errorf("scheme mismatch: got %s, want %s", parsed.Scheme, original.Scheme)
	}
	if parsed.Payment.TxID != original.Payment.TxID {
		t.Errorf("payment.txid mismatch: got %s, want %s", parsed.Payment.TxID, original.Payment.TxID)
	}
	if parsed.Payment.RawTxB64 != original.Payment.RawTxB64 {
		t.Errorf("payment.rawtx_b64 mismatch: got %s, want %s", parsed.Payment.RawTxB64, original.Payment.RawTxB64)
	}
	if parsed.ChallengeSHA256 != original.ChallengeSHA256 {
		t.Errorf("challenge_sha256 mismatch: got %s, want %s", parsed.ChallengeSHA256, original.ChallengeSHA256)
	}
}

func TestParseProofEmpty(t *testing.T) {
	_, err := ParseProof("")
	if err == nil {
		t.Error("expected error for empty header")
	}
}

func TestParseProofMissingRawTx(t *testing.T) {
	// Valid base64url JSON but missing payment.rawtx_b64
	proof := &Proof{
		V:               1,
		Scheme:          "bsv-tx-v1",
		ChallengeSHA256: "abc123",
		Payment: Payment{
			TxID: strings.Repeat("a", 64),
		},
	}
	encoded, _ := EncodeProof(proof)
	_, err := ParseProof(encoded)
	if err == nil {
		t.Error("expected error for missing payment.rawtx_b64")
	}
}

func TestParseProofMissingTxID(t *testing.T) {
	// Valid base64url JSON but missing payment.txid — spec §5 requires it.
	proof := &Proof{
		V:               1,
		Scheme:          "bsv-tx-v1",
		ChallengeSHA256: "abc123",
		Payment: Payment{
			RawTxB64: "AQAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
		},
	}
	encoded, _ := EncodeProof(proof)
	_, err := ParseProof(encoded)
	if err == nil {
		t.Error("expected error for missing payment.txid")
	}
	if err != nil && !strings.Contains(err.Error(), "payment.txid") {
		t.Errorf("expected error about payment.txid, got: %v", err)
	}
}

func TestParseProofMissingChallengeHash(t *testing.T) {
	// Valid base64url JSON but missing challenge_sha256
	proof := &Proof{
		V:      1,
		Scheme: "bsv-tx-v1",
		Payment: Payment{
			TxID:     strings.Repeat("a", 64),
			RawTxB64: "AQAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
		},
	}
	encoded, _ := EncodeProof(proof)
	_, err := ParseProof(encoded)
	if err == nil {
		t.Error("expected error for missing challenge_sha256")
	}
}

func TestParseProofCompactPrefix(t *testing.T) {
	// Test v1.bsv-tx.<base64url> compact format
	original := &Proof{
		V:               1,
		Scheme:          "bsv-tx-v1",
		ChallengeSHA256: "deadbeef",
		Payment: Payment{
			TxID:     strings.Repeat("b", 64),
			RawTxB64: "AQAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
		},
	}

	encoded, err := EncodeProof(original)
	if err != nil {
		t.Fatalf("EncodeProof: %v", err)
	}

	// Prepend compact prefix
	compactEncoded := "v1.bsv-tx." + encoded

	parsed, err := ParseProof(compactEncoded)
	if err != nil {
		t.Fatalf("ParseProof with compact prefix: %v", err)
	}

	if parsed.Payment.TxID != original.Payment.TxID {
		t.Errorf("payment.txid mismatch: got %s, want %s", parsed.Payment.TxID, original.Payment.TxID)
	}
}

// TestHTTPStatusForError validates the spec §9 error-to-HTTP-status mapping.
func TestHTTPStatusForError(t *testing.T) {
	tests := []struct {
		code   ErrorCode
		status int
	}{
		{ErrInvalidVersion, 400},
		{ErrInvalidScheme, 400},
		{ErrInvalidProof, 400},
		{ErrChallengeNotFound, 400},
		{ErrNonceMissing, 400},
		{ErrExpiredChallenge, 402},  // spec §9: "Expired challenge → 402"
		{ErrInsufficientAmount, 402}, // spec §9: "Insufficient payment → 402"
		{ErrDoubleSpend, 402},       // spec §9: "Nonce already spent → 402"
		{ErrMempoolRejected, 402},   // spec §9: "Nonce already spent → 402"
		{ErrInvalidBinding, 400},    // spec §9: "Request binding mismatch → 400"
		{ErrInvalidPayee, 400},      // spec §9: "Invalid transaction → 400"
		{ErrNoUTXOsAvailable, 503},
		{ErrMempoolError, 503},
		{ErrInternalError, 500},
	}

	for _, tt := range tests {
		got := HTTPStatusForError(tt.code)
		if got != tt.status {
			t.Errorf("HTTPStatusForError(%s): got %d, want %d", tt.code, got, tt.status)
		}
	}
}
