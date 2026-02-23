package nonce

import (
	"context"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/transaction"
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

// testBroadcaster is a mock broadcaster for tests.
type testBroadcaster struct {
	lastTx *transaction.Transaction
}

func (tb *testBroadcaster) Broadcast(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	tb.lastTx = tx
	return &transaction.BroadcastSuccess{
		Txid:    tx.TxID().String(),
		Message: "test broadcast",
	}, nil
}

func (tb *testBroadcaster) BroadcastCtx(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	return tb.Broadcast(tx)
}

func newTestPool(t *testing.T) (*Pool, *testBroadcaster) {
	t.Helper()

	key, err := ec.NewPrivateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	bc := &testBroadcaster{}
	pool, err := NewPool(key, false, 5*time.Minute, bc)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	return pool, bc
}

func TestNewPool(t *testing.T) {
	pool, _ := newTestPool(t)

	if pool.Address() == "" {
		t.Error("expected non-empty address")
	}
	if pool.Available() != 0 {
		t.Errorf("expected 0 available, got %d", pool.Available())
	}
}

func TestAddExistingAndLease(t *testing.T) {
	pool, _ := newTestPool(t)

	scriptHex, err := pool.LockingScriptHex()
	if err != nil {
		t.Fatalf("locking script: %v", err)
	}

	// Add 3 nonce UTXOs
	utxos := []NonceUTXO{
		{TxID: "aaaa" + repeatHex("a", 60), Vout: 0, Script: scriptHex, Satoshis: 1},
		{TxID: "aaaa" + repeatHex("a", 60), Vout: 1, Script: scriptHex, Satoshis: 1},
		{TxID: "bbbb" + repeatHex("b", 60), Vout: 0, Script: scriptHex, Satoshis: 1},
	}
	pool.AddExisting(utxos)

	if pool.Available() != 3 {
		t.Errorf("expected 3 available, got %d", pool.Available())
	}

	// Lease one
	leased, err := pool.Lease()
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if leased.Status != StatusLeased {
		t.Errorf("expected status leased, got %s", leased.Status)
	}
	if pool.Available() != 2 {
		t.Errorf("expected 2 available after lease, got %d", pool.Available())
	}
}

func TestLeaseExhaustion(t *testing.T) {
	pool, _ := newTestPool(t)

	scriptHex, _ := pool.LockingScriptHex()

	pool.AddExisting([]NonceUTXO{
		{TxID: repeatHex("c", 64), Vout: 0, Script: scriptHex, Satoshis: 1},
	})

	// First lease should succeed
	_, err := pool.Lease()
	if err != nil {
		t.Fatalf("first lease failed: %v", err)
	}

	// Second lease should fail (pool exhausted)
	_, err = pool.Lease()
	if err == nil {
		t.Error("expected error on exhausted pool, got nil")
	}
}

func TestMarkSpentAndLookup(t *testing.T) {
	pool, _ := newTestPool(t)

	scriptHex, _ := pool.LockingScriptHex()
	txid := repeatHex("d", 64)

	pool.AddExisting([]NonceUTXO{
		{TxID: txid, Vout: 0, Script: scriptHex, Satoshis: 1},
	})

	// Lookup
	n := pool.Lookup(txid, 0)
	if n == nil {
		t.Fatal("expected to find nonce")
	}
	if n.Status != StatusAvailable {
		t.Errorf("expected available, got %s", n.Status)
	}

	// Mark spent
	pool.MarkSpent(txid, 0)
	n = pool.Lookup(txid, 0)
	if n.Status != StatusSpent {
		t.Errorf("expected spent, got %s", n.Status)
	}
}

func TestReclaim(t *testing.T) {
	key, _ := ec.NewPrivateKey()
	bc := &testBroadcaster{}
	pool, _ := NewPool(key, false, 1*time.Millisecond, bc) // very short TTL

	scriptHex, _ := pool.LockingScriptHex()
	pool.AddExisting([]NonceUTXO{
		{TxID: repeatHex("e", 64), Vout: 0, Script: scriptHex, Satoshis: 1},
	})

	// Lease it
	_, err := pool.Lease()
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	if pool.Available() != 0 {
		t.Errorf("expected 0 available after lease, got %d", pool.Available())
	}

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	// Reclaim
	reclaimed := pool.Reclaim()
	if reclaimed != 1 {
		t.Errorf("expected 1 reclaimed, got %d", reclaimed)
	}
	if pool.Available() != 1 {
		t.Errorf("expected 1 available after reclaim, got %d", pool.Available())
	}
}

func TestStats(t *testing.T) {
	pool, _ := newTestPool(t)

	scriptHex, _ := pool.LockingScriptHex()
	pool.AddExisting([]NonceUTXO{
		{TxID: repeatHex("f", 64), Vout: 0, Script: scriptHex, Satoshis: 1},
		{TxID: repeatHex("f", 64), Vout: 1, Script: scriptHex, Satoshis: 1},
		{TxID: repeatHex("f", 64), Vout: 2, Script: scriptHex, Satoshis: 1},
	})

	// Lease one
	pool.Lease()

	// Mark one spent
	pool.MarkSpent(repeatHex("f", 64), 2)

	stats := pool.Stats()
	if stats.Total != 3 {
		t.Errorf("expected total 3, got %d", stats.Total)
	}
	if stats.Available != 1 {
		t.Errorf("expected 1 available, got %d", stats.Available)
	}
	if stats.Leased != 1 {
		t.Errorf("expected 1 leased, got %d", stats.Leased)
	}
	if stats.Spent != 1 {
		t.Errorf("expected 1 spent, got %d", stats.Spent)
	}
}

func repeatHex(ch string, length int) string {
	s := ""
	for len(s) < length {
		s += ch
	}
	return s[:length]
}
