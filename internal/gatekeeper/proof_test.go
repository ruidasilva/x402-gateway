package gatekeeper

import (
	"testing"
)

func TestParseAndEncodeProof(t *testing.T) {
	original := &Proof{
		PartialTxHex:  "0100000001" + "aa" + "00000000",
		ChallengeHash: "abc123def456",
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

	if parsed.PartialTxHex != original.PartialTxHex {
		t.Errorf("partial_tx mismatch: got %s, want %s", parsed.PartialTxHex, original.PartialTxHex)
	}
	if parsed.ChallengeHash != original.ChallengeHash {
		t.Errorf("challenge_hash mismatch: got %s, want %s", parsed.ChallengeHash, original.ChallengeHash)
	}
}

func TestParseProofEmpty(t *testing.T) {
	_, err := ParseProof("")
	if err == nil {
		t.Error("expected error for empty header")
	}
}

func TestParseProofMissingFields(t *testing.T) {
	// Valid base64url JSON but missing required fields
	encoded, _ := EncodeProof(&Proof{PartialTxHex: "", ChallengeHash: "abc"})
	_, err := ParseProof(encoded)
	if err == nil {
		t.Error("expected error for missing partial_tx")
	}
}
