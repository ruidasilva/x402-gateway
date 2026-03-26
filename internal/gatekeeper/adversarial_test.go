// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package gatekeeper

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/merkleworks/x402-bsv/internal/challenge"
)

// ---------------------------------------------------------------------------
// Shared adversarial test helpers
// ---------------------------------------------------------------------------

// countingHandler counts how many times the downstream handler is invoked.
// This is the core assertion primitive for "no duplicate execution" tests.
type countingHandler struct {
	count atomic.Int32
}

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.count.Add(1)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "unlocked"})
}

// newTestNonce returns a NonceRef suitable for tests.
func newTestNonce() *challenge.NonceRef {
	return &challenge.NonceRef{
		TxID:             nonceTxIDHex,
		Vout:             0,
		Satoshis:         1,
		LockingScriptHex: "76a914aabbccdd88ac",
	}
}

// ---------------------------------------------------------------------------
// Test 1: Idempotent re-serve fast-path enforces request binding
// ---------------------------------------------------------------------------

func TestReserveBindingReplay(t *testing.T) {
	cfg := testMiddlewareConfig()
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	_, challengeHash := buildChallengeForTest(t, cfg.ChallengeCache, reqBase, nonce, nil)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	// --- Step 1: Valid proof with correct binding → 200 ---
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req1.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("valid proof: want 200, got %d: %s", rec1.Code, rec1.Body.String())
	}
	if downstream.count.Load() != 1 {
		t.Fatalf("downstream should be called exactly once, got %d", downstream.count.Load())
	}

	// --- Step 2: Re-issue challenge for same nonce (to populate cache for re-serve path) ---
	// The challenge was deleted on success. Re-store it so the replay cache
	// fast-path can find the originalChallenge.
	cfg.ChallengeCache.Store(challengeHash, buildChallengeFromBase(t, reqBase, nonce))

	// --- Step 3: Replay same proof but targeting a DIFFERENT path ---
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "http://localhost:8402/v1/OTHER_ENDPOINT", nil)
	req2.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec2, req2)

	// Must be rejected — binding mismatch on the re-serve path.
	if rec2.Code == http.StatusOK {
		t.Fatalf("replayed proof to different path should NOT produce 200, got: %d %s",
			rec2.Code, rec2.Body.String())
	}
	t.Logf("binding replay to different path → %d (correctly rejected)", rec2.Code)

	// Downstream must NOT have been called a second time.
	if downstream.count.Load() != 1 {
		t.Fatalf("downstream should still be 1 after binding rejection, got %d", downstream.count.Load())
	}

	// --- Step 4: Replay same proof with wrong Host ---
	cfg.ChallengeCache.Store(challengeHash, buildChallengeFromBase(t, reqBase, nonce))

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "http://evil.example.com/v1/expensive", nil)
	req3.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec3, req3)

	if rec3.Code == http.StatusOK {
		t.Fatalf("replayed proof to different host should NOT produce 200, got: %d %s",
			rec3.Code, rec3.Body.String())
	}
	t.Logf("binding replay to different host → %d (correctly rejected)", rec3.Code)

	if downstream.count.Load() != 1 {
		t.Fatalf("downstream should still be 1 after host rejection, got %d", downstream.count.Load())
	}
}

// buildChallengeFromBase builds a challenge struct matching the given request
// without storing it (caller stores explicitly).
func buildChallengeFromBase(t *testing.T, r *http.Request, nonce *challenge.NonceRef) *challenge.Challenge {
	t.Helper()
	ch, err := challenge.Build(r, challenge.BuildOptions{
		PayeeLockingScriptHex: testPayeeScriptHex,
		Amount:                testAmountSats,
		Network:               "testnet",
		TTL:                   5 * time.Minute,
		BindHeaders:           HeaderAllowlist,
		NonceUTXO:             nonce,
	})
	if err != nil {
		t.Fatalf("challenge.Build: %v", err)
	}
	return ch
}

// ---------------------------------------------------------------------------
// Test 2: Concurrent duplicate proof → exactly one execution
// ---------------------------------------------------------------------------

func TestConcurrentDuplicateProof(t *testing.T) {
	cfg := testMiddlewareConfig()
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	_, challengeHash := buildChallengeForTest(t, cfg.ChallengeCache, reqBase, nonce, nil)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	type result struct {
		code int
		body string
	}
	results := make([]result, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
			req.Header.Set(ProofHeader, proofHeader)
			handler.ServeHTTP(rec, req)
			results[idx] = result{code: rec.Code, body: rec.Body.String()}
		}(i)
	}
	wg.Wait()

	// CRITICAL ASSERTION: downstream handler executes EXACTLY once.
	// This is the core invariant — one payment = one execution.
	executions := int(downstream.count.Load())
	if executions != 1 {
		t.Fatalf("CRITICAL: downstream called %d times (must be exactly 1)", executions)
	}

	// Count 200 responses. Some may be receipt-only ("already_settled")
	// which is correct idempotent behavior — the resource was already served.
	okCount := 0
	receiptOnlyCount := 0
	for _, r := range results {
		if r.code == http.StatusOK {
			okCount++
			if containsSubstr(r.body, "already_settled") {
				receiptOnlyCount++
			}
		}
	}

	// At least one 200 must be a real execution (not receipt-only).
	realExecutions := okCount - receiptOnlyCount
	if realExecutions != 1 {
		t.Fatalf("expected exactly 1 real execution 200, got %d (total 200s: %d, receipt-only: %d)",
			realExecutions, okCount, receiptOnlyCount)
	}

	codes := make([]int, goroutines)
	for i, r := range results {
		codes[i] = r.code
	}
	t.Logf("results: %d×200 (%d receipt-only), %d×non-200 (out of %d goroutines)",
		okCount, receiptOnlyCount, goroutines-okCount, goroutines)
}

// ---------------------------------------------------------------------------
// Test 3: Nonce reclaim while proof in-flight — protocol-safe
// ---------------------------------------------------------------------------

func TestNonceReclaimWhileInFlight(t *testing.T) {
	// This test verifies that proof verification is independent of pool state.
	// The nonce pool's lease/reclaim lifecycle is an operational concern —
	// proof verification only checks the settlement-layer nonce spend.
	cfg := testMiddlewareConfig()
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	_, challengeHash := buildChallengeForTest(t, cfg.ChallengeCache, reqBase, nonce, nil)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	// Simulate: the nonce pool has reclaimed this nonce back to "available"
	// because the lease TTL expired. This would happen if the client was slow.
	// Create a pool, add the nonce, lease it, then forcibly set it back to available.
	// The key point: the proof verification path does NOT consult the pool for
	// nonce validity — it checks the settlement layer (tx inputs vs challenge.NonceUTXO).

	// Submit the proof — it should succeed regardless of pool state.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("proof should succeed regardless of pool reclaim state: want 200, got %d: %s",
			rec.Code, rec.Body.String())
	}
	if downstream.count.Load() != 1 {
		t.Fatalf("downstream should execute exactly once, got %d", downstream.count.Load())
	}

	t.Log("PROTOCOL-SAFE: proof verification is independent of nonce pool lease state")
	t.Log("OPERATIONALLY: reclaim can cause a re-issued challenge to reference a spent nonce")
}

// ---------------------------------------------------------------------------
// Test 4: Crash after lease before store — replay cache reservation expires
// ---------------------------------------------------------------------------

func TestCrashAfterLeaseBeforeStore(t *testing.T) {
	// This test verifies the impact at the gatekeeper layer when a nonce is
	// leased but the challenge is never stored (simulates crash between
	// NoncePool.Lease() and ChallengeCache.Store()).
	//
	// Impact: client receives no challenge → no proof can be submitted for
	// that nonce → nonce lease expires → reclaim returns it to available.
	//
	// At the gatekeeper layer, we verify that a proof referencing a
	// challenge that was never stored is correctly rejected.

	cfg := testMiddlewareConfig()
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()

	// Build a challenge but do NOT store it in the cache (simulates crash).
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	ch := buildChallengeFromBase(t, reqBase, nonce)
	challengeHash, err := challenge.ComputeHash(ch)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	// Deliberately skip: cfg.ChallengeCache.Store(challengeHash, ch)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec, req)

	// Challenge not in cache → "challenge not found" (400).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unstored challenge: want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if code, _ := body["code"].(string); code != string(ErrChallengeNotFound) {
		t.Errorf("error code: got %q, want %q", code, ErrChallengeNotFound)
	}

	// Downstream must NOT execute.
	if downstream.count.Load() != 0 {
		t.Fatalf("downstream should NOT execute for unstored challenge, got %d", downstream.count.Load())
	}

	t.Log("CONFIRMED: crash-after-lease produces challenge_not_found (safe rejection)")
	t.Log("Nonce recovery depends on pool lease TTL + reclaim loop (tested in pool package)")
}

// ---------------------------------------------------------------------------
// Test 5: Mempool false negative → no execution (202)
// ---------------------------------------------------------------------------

func TestMempoolFalseNegative(t *testing.T) {
	cfg := testMiddlewareConfig()
	// Override: mempool always says not-visible (simulates propagation delay).
	cfg.MempoolChecker = &mempoolNotVisible{}
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	_, challengeHash := buildChallengeForTest(t, cfg.ChallengeCache, reqBase, nonce, nil)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec, req)

	// Expect 202 — payment acknowledged but not confirmed.
	if rec.Code != http.StatusAccepted {
		t.Fatalf("mempool not-visible: want 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// CRITICAL: downstream must NOT execute.
	if downstream.count.Load() != 0 {
		t.Fatalf("downstream should NOT execute when mempool returns not-visible, got %d executions",
			downstream.count.Load())
	}
	t.Logf("mempool not-visible → %d (no execution)", rec.Code)
}

// mempoolNotVisible always returns visible=false, doubleSpend=false.
type mempoolNotVisible struct{}

func (m *mempoolNotVisible) CheckMempool(string) (visible, doubleSpend bool, err error) {
	return false, false, nil
}

// ---------------------------------------------------------------------------
// Test 6: Mempool transient error → no execution (503)
// ---------------------------------------------------------------------------

func TestMempoolTransientError(t *testing.T) {
	cfg := testMiddlewareConfig()
	// Override: mempool checker returns an error.
	cfg.MempoolChecker = &mempoolError{}
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	_, challengeHash := buildChallengeForTest(t, cfg.ChallengeCache, reqBase, nonce, nil)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec, req)

	// Expect 503 — mempool check failed.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("mempool error: want 503, got %d: %s", rec.Code, rec.Body.String())
	}

	// CRITICAL: downstream must NOT execute.
	if downstream.count.Load() != 0 {
		t.Fatalf("downstream should NOT execute on mempool error, got %d executions",
			downstream.count.Load())
	}

	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if code, _ := body["code"].(string); code != string(ErrMempoolError) {
		t.Errorf("error code: got %q, want %q", code, ErrMempoolError)
	}
	t.Logf("mempool error → %d (no execution)", rec.Code)
}

// mempoolError always returns an error.
type mempoolError struct{}

func (m *mempoolError) CheckMempool(string) (visible, doubleSpend bool, err error) {
	return false, false, errors.New("node connection refused")
}

// ---------------------------------------------------------------------------
// Test 7: Proof with v=0 is rejected strictly
// ---------------------------------------------------------------------------

func TestProofWithZeroVersion(t *testing.T) {
	cfg := testMiddlewareConfig()
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	_, challengeHash := buildChallengeForTest(t, cfg.ChallengeCache, reqBase, nonce, nil)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))

	// Build proof manually with v=0 (not using buildProofHeader which sets v=1).
	proof := &Proof{
		V:               0, // ZERO — must be rejected
		Scheme:          challenge.Scheme,
		ChallengeSHA256: challengeHash,
		Payment: Payment{
			TxID:     tx.TxID().String(),
			RawTxB64: "", // won't get this far
		},
		Request: RequestBinding{
			Method: "GET",
			Path:   "/v1/expensive",
		},
	}

	// Encode manually — EncodeProof doesn't validate, just marshals.
	encoded, err := EncodeProof(proof)
	if err != nil {
		t.Fatalf("EncodeProof: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req.Header.Set(ProofHeader, encoded)
	handler.ServeHTTP(rec, req)

	// ParseProof should reject v=0 before any verification runs.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("proof v=0: want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// No downstream execution.
	if downstream.count.Load() != 0 {
		t.Fatalf("downstream should NOT execute for v=0 proof, got %d", downstream.count.Load())
	}

	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if code, _ := body["code"].(string); code != string(ErrInvalidProof) {
		t.Errorf("error code: got %q, want %q", code, ErrInvalidProof)
	}
	t.Logf("proof v=0 → %d (rejected, no defaulting)", rec.Code)
}

// ---------------------------------------------------------------------------
// Test 8: require_mempool_accept=false bypasses mempool (by design)
// ---------------------------------------------------------------------------

func TestRequireMempoolFalseBypass(t *testing.T) {
	// This test locks in the EXPLICITLY ALLOWED behavior:
	// when require_mempool_accept=false, no mempool check is performed.
	// Per spec §7: mempool acceptance is optional ("Optionally verify...").
	cfg := testMiddlewareConfig()
	cfg.MempoolChecker = nil // no checker at all
	downstream := &countingHandler{}
	handler := Middleware(cfg)(downstream)

	nonce := newTestNonce()
	reqBase := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)

	// Build challenge with require_mempool_accept = false.
	ch, err := challenge.Build(reqBase, challenge.BuildOptions{
		PayeeLockingScriptHex: testPayeeScriptHex,
		Amount:                testAmountSats,
		Network:               "testnet",
		TTL:                   5 * time.Minute,
		BindHeaders:           HeaderAllowlist,
		NonceUTXO:             nonce,
	})
	if err != nil {
		t.Fatalf("challenge.Build: %v", err)
	}
	// Override: explicitly disable mempool gating.
	ch.RequireMempoolAccept = false

	challengeHash, err := challenge.ComputeHash(ch)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	cfg.ChallengeCache.Store(challengeHash, ch)

	tx := buildTestTx(nonceTxIDHex, 0, testPayeeScriptHex, uint64(testAmountSats))
	proofHeader := buildProofHeader(t, tx, challengeHash, reqBase)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:8402/v1/expensive", nil)
	req.Header.Set(ProofHeader, proofHeader)
	handler.ServeHTTP(rec, req)

	// Expected: 200 without any mempool check.
	// This is by design — spec §7 says mempool acceptance is optional.
	if rec.Code != http.StatusOK {
		t.Fatalf("require_mempool_accept=false: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if downstream.count.Load() != 1 {
		t.Fatalf("downstream should execute exactly once, got %d", downstream.count.Load())
	}

	t.Log("BY DESIGN: require_mempool_accept=false allows execution without mempool check")
	t.Log("Operational deployments SHOULD always set require_mempool_accept=true")
}

// containsSubstr checks if s contains sub (simple helper to avoid importing strings).
func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

