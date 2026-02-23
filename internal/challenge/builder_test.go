package challenge

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/merkle-works/x402-gateway/internal/nonce"
)

func TestBuildAndVerify(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive?foo=bar", nil)
	req.Header.Set("Content-Type", "application/json")

	nonceUTXO := &nonce.NonceUTXO{
		TxID:     strings.Repeat("a", 64),
		Vout:     0,
		Script:   "76a91489abcdefab89abcdefab89abcdefab89abcdefab88ac",
		Satoshis: 1,
	}

	opts := BuildOptions{
		PayeeAddress: "mfWxJ45yp2SFn7UciZyNpvDKrzbi36LaVX",
		Amount:       100,
		Network:      "testnet",
		TTL:          5 * time.Minute,
		BindHeaders:  []string{"Content-Type"},
	}

	ch, err := Build(req, nonceUTXO, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Verify structure
	if ch.Scheme != Scheme {
		t.Errorf("scheme: got %s, want %s", ch.Scheme, Scheme)
	}
	if ch.Version != Version {
		t.Errorf("version: got %s, want %s", ch.Version, Version)
	}
	if ch.Network != "testnet" {
		t.Errorf("network: got %s, want testnet", ch.Network)
	}
	if ch.Amount != 100 {
		t.Errorf("amount: got %d, want 100", ch.Amount)
	}
	if ch.Payee != opts.PayeeAddress {
		t.Errorf("payee: got %s, want %s", ch.Payee, opts.PayeeAddress)
	}
	if ch.Nonce.TxID != nonceUTXO.TxID {
		t.Errorf("nonce txid: got %s, want %s", ch.Nonce.TxID, nonceUTXO.TxID)
	}
	if ch.ChallengeSHA256 == "" {
		t.Error("challenge hash should not be empty")
	}
	if ch.Expiry <= time.Now().Unix() {
		t.Error("expiry should be in the future")
	}

	// Verify request binding
	if ch.RequestBinding.Method != "GET" {
		t.Errorf("method: got %s, want GET", ch.RequestBinding.Method)
	}
	if ch.RequestBinding.Path != "/v1/expensive" {
		t.Errorf("path: got %s, want /v1/expensive", ch.RequestBinding.Path)
	}
	if ch.RequestBinding.Domain != "localhost:8402" {
		t.Errorf("domain: got %s, want localhost:8402", ch.RequestBinding.Domain)
	}

	// Verify challenge hash
	valid, err := Verify(ch)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Error("challenge hash verification failed")
	}
}

func TestEncodeAndDecode(t *testing.T) {
	ch := &Challenge{
		Scheme:          Scheme,
		Network:         "testnet",
		Version:         Version,
		Amount:          200,
		Payee:           "mfWxJ45yp2SFn7UciZyNpvDKrzbi36LaVX",
		Expiry:          time.Now().Add(5 * time.Minute).Unix(),
		ChallengeSHA256: "abc123",
		Nonce: NonceRef{
			TxID:     strings.Repeat("b", 64),
			Vout:     1,
			Script:   "76a91489abcdefab88ac",
			Satoshis: 1,
		},
		RequestBinding: RequestBinding{
			Method: "POST",
			Path:   "/api/data",
			Domain: "example.com",
		},
	}

	encoded, err := Encode(ch)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoded == "" {
		t.Error("encoded should not be empty")
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.Scheme != ch.Scheme {
		t.Errorf("scheme mismatch: got %s, want %s", decoded.Scheme, ch.Scheme)
	}
	if decoded.Amount != ch.Amount {
		t.Errorf("amount mismatch: got %d, want %d", decoded.Amount, ch.Amount)
	}
	if decoded.Nonce.TxID != ch.Nonce.TxID {
		t.Errorf("nonce txid mismatch")
	}
}

func TestHashBody(t *testing.T) {
	h1 := HashBody([]byte("hello"))
	h2 := HashBody([]byte("hello"))
	h3 := HashBody([]byte("world"))

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("hash length: got %d, want 64", len(h1))
	}
}

func TestHashBodyEmpty(t *testing.T) {
	h := HashBody(nil)
	if h == "" {
		t.Error("hash of nil should not be empty")
	}
	if len(h) != 64 {
		t.Errorf("hash length: got %d, want 64", len(h))
	}
}
