package replay

import (
	"strings"
	"testing"
	"time"
)

func TestRecordAndCheck(t *testing.T) {
	cache := New(5*time.Minute, 100)

	nonceTxid := strings.Repeat("a", 64)
	var nonceVout uint32 = 0
	spendTxid := strings.Repeat("b", 64)
	challengeHash := strings.Repeat("c", 64)

	// Initially not found
	_, _, found := cache.Check(nonceTxid, nonceVout)
	if found {
		t.Error("expected not found")
	}

	// Record
	cache.Record(nonceTxid, nonceVout, spendTxid, challengeHash)

	// Now found
	resultTxid, resultHash, found := cache.Check(nonceTxid, nonceVout)
	if !found {
		t.Error("expected found after record")
	}
	if resultTxid != spendTxid {
		t.Errorf("spendTxID: got %s, want %s", resultTxid, spendTxid)
	}
	if resultHash != challengeHash {
		t.Errorf("challengeHash: got %s, want %s", resultHash, challengeHash)
	}
}

func TestDifferentVout(t *testing.T) {
	cache := New(5*time.Minute, 100)

	txid := strings.Repeat("a", 64)
	cache.Record(txid, 0, "spend0", "hash0")
	cache.Record(txid, 1, "spend1", "hash1")

	// Different vout = different entry
	result0, _, found0 := cache.Check(txid, 0)
	result1, _, found1 := cache.Check(txid, 1)
	_, _, found2 := cache.Check(txid, 2)

	if !found0 || result0 != "spend0" {
		t.Errorf("vout 0: got %s/%v, want spend0/true", result0, found0)
	}
	if !found1 || result1 != "spend1" {
		t.Errorf("vout 1: got %s/%v, want spend1/true", result1, found1)
	}
	if found2 {
		t.Error("vout 2 should not be found")
	}
}

func TestTTLExpiry(t *testing.T) {
	cache := New(1*time.Millisecond, 100)

	nonceTxid := strings.Repeat("c", 64)
	cache.Record(nonceTxid, 0, "spend1", "hash1")

	time.Sleep(5 * time.Millisecond)

	_, _, found := cache.Check(nonceTxid, 0)
	if found {
		t.Error("expected entry to expire after TTL")
	}
}

func TestLRUEviction(t *testing.T) {
	cache := New(5*time.Minute, 2) // max 2 entries

	txid1 := "tx1" + strings.Repeat("0", 61)
	txid2 := "tx2" + strings.Repeat("0", 61)
	txid3 := "tx3" + strings.Repeat("0", 61)

	cache.Record(txid1, 0, "spend1", "hash1")
	cache.Record(txid2, 0, "spend2", "hash2")

	// Both should exist
	_, _, found1 := cache.Check(txid1, 0)
	_, _, found2 := cache.Check(txid2, 0)
	if !found1 || !found2 {
		t.Error("expected both entries to exist")
	}

	// Adding a third should evict the first (LRU)
	cache.Record(txid3, 0, "spend3", "hash3")

	_, _, found1 = cache.Check(txid1, 0)
	if found1 {
		t.Error("expected first entry to be evicted")
	}

	_, _, found3 := cache.Check(txid3, 0)
	if !found3 {
		t.Error("expected third entry to exist")
	}
}

func TestCleanup(t *testing.T) {
	cache := New(1*time.Millisecond, 100)

	cache.Record(strings.Repeat("d", 64), 0, "spend1", "hash1")
	cache.Record(strings.Repeat("e", 64), 0, "spend2", "hash2")

	time.Sleep(5 * time.Millisecond)

	removed := cache.Cleanup()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	if cache.Size() != 0 {
		t.Errorf("expected size 0 after cleanup, got %d", cache.Size())
	}
}

func TestSize(t *testing.T) {
	cache := New(5*time.Minute, 100)

	if cache.Size() != 0 {
		t.Errorf("expected size 0, got %d", cache.Size())
	}

	cache.Record(strings.Repeat("f", 64), 0, "spend1", "hash1")
	if cache.Size() != 1 {
		t.Errorf("expected size 1, got %d", cache.Size())
	}
}
