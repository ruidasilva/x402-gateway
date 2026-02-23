package replay

import (
	"strings"
	"testing"
	"time"
)

func TestRecordAndCheck(t *testing.T) {
	cache := New(5*time.Minute, 100)

	txid := strings.Repeat("a", 64)
	spendTxid := strings.Repeat("b", 64)

	// Initially not found
	_, found := cache.Check(txid, 0)
	if found {
		t.Error("expected not found")
	}

	// Record
	cache.Record(txid, 0, spendTxid)

	// Now found
	result, found := cache.Check(txid, 0)
	if !found {
		t.Error("expected found after record")
	}
	if result != spendTxid {
		t.Errorf("got %s, want %s", result, spendTxid)
	}
}

func TestTTLExpiry(t *testing.T) {
	cache := New(1*time.Millisecond, 100)

	txid := strings.Repeat("c", 64)
	cache.Record(txid, 0, "spend1")

	time.Sleep(5 * time.Millisecond)

	_, found := cache.Check(txid, 0)
	if found {
		t.Error("expected entry to expire after TTL")
	}
}

func TestLRUEviction(t *testing.T) {
	cache := New(5*time.Minute, 2) // max 2 entries

	cache.Record("tx1"+strings.Repeat("0", 61), 0, "spend1")
	cache.Record("tx2"+strings.Repeat("0", 61), 0, "spend2")

	// Both should exist
	_, found1 := cache.Check("tx1"+strings.Repeat("0", 61), 0)
	_, found2 := cache.Check("tx2"+strings.Repeat("0", 61), 0)
	if !found1 || !found2 {
		t.Error("expected both entries to exist")
	}

	// Adding a third should evict the first (LRU)
	cache.Record("tx3"+strings.Repeat("0", 61), 0, "spend3")

	_, found1 = cache.Check("tx1"+strings.Repeat("0", 61), 0)
	if found1 {
		t.Error("expected first entry to be evicted")
	}

	_, found3 := cache.Check("tx3"+strings.Repeat("0", 61), 0)
	if !found3 {
		t.Error("expected third entry to exist")
	}
}

func TestCleanup(t *testing.T) {
	cache := New(1*time.Millisecond, 100)

	cache.Record(strings.Repeat("d", 64), 0, "spend1")
	cache.Record(strings.Repeat("e", 64), 0, "spend2")

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

	cache.Record(strings.Repeat("f", 64), 0, "spend1")
	if cache.Size() != 1 {
		t.Errorf("expected size 1, got %d", cache.Size())
	}
}
