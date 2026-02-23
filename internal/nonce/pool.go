package nonce

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	sighash "github.com/bsv-blockchain/go-sdk/transaction/sighash"
	p2pkh "github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"
)

// Pool manages an in-memory pool of 1-sat nonce UTXOs.
// The pool is backed by a delegator private key that owns all nonce UTXOs.
type Pool struct {
	mu          sync.Mutex
	utxos       map[string]*NonceUTXO // key: "txid:vout"
	key         *ec.PrivateKey
	address     *script.Address
	mainnet     bool
	leaseTTL    time.Duration
	broadcaster transaction.Broadcaster
	logger      *slog.Logger
}

// NewPool creates a new nonce pool.
func NewPool(key *ec.PrivateKey, mainnet bool, leaseTTL time.Duration, broadcaster transaction.Broadcaster) (*Pool, error) {
	addr, err := script.NewAddressFromPublicKey(key.PubKey(), mainnet)
	if err != nil {
		return nil, fmt.Errorf("derive address: %w", err)
	}

	p := &Pool{
		utxos:       make(map[string]*NonceUTXO),
		key:         key,
		address:     addr,
		mainnet:     mainnet,
		leaseTTL:    leaseTTL,
		broadcaster: broadcaster,
		logger:      slog.Default().With("component", "nonce-pool"),
	}

	return p, nil
}

// Address returns the BSV address that owns all nonce UTXOs.
func (p *Pool) Address() string {
	return p.address.AddressString
}

// LockingScriptHex returns the P2PKH locking script for the pool address.
func (p *Pool) LockingScriptHex() (string, error) {
	s, err := p.lockingScript()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(*s), nil
}

func (p *Pool) lockingScript() (*script.Script, error) {
	return p2pkh.Lock(p.address)
}

// Mint creates a fanout transaction that produces `count` new 1-sat nonce UTXOs.
// It requires a funding UTXO to pay for the outputs + miner fee.
// The funding UTXO must belong to the same key that owns the nonce pool.
func (p *Pool) Mint(fundingTxID string, fundingVout uint32, fundingScript string, fundingSatoshis uint64, count int) ([]NonceUTXO, error) {
	if count <= 0 || count > 10000 {
		return nil, fmt.Errorf("count must be between 1 and 10000, got %d", count)
	}

	// Build the fanout transaction
	tx := transaction.NewTransaction()

	// Signing template for the funding input (standard SIGHASH_ALL|FORKID)
	allForkID := sighash.AllForkID
	unlocker, err := p2pkh.Unlock(p.key, &allForkID)
	if err != nil {
		return nil, fmt.Errorf("create unlocker: %w", err)
	}

	// Add funding input
	err = tx.AddInputFrom(fundingTxID, fundingVout, fundingScript, fundingSatoshis, unlocker)
	if err != nil {
		return nil, fmt.Errorf("add funding input: %w", err)
	}

	// Add count × 1-sat outputs to the pool address
	for i := 0; i < count; i++ {
		if err := tx.PayToAddress(p.Address(), 1); err != nil {
			return nil, fmt.Errorf("add nonce output %d: %w", i, err)
		}
	}

	// Calculate fee: estimate tx size × 1 sat/byte
	// Each P2PKH output is ~34 bytes, each P2PKH input is ~148 bytes, overhead ~10 bytes
	estimatedSize := 10 + 148 + (count * 34)
	requiredSats := uint64(count) + uint64(estimatedSize) // 1 sat per output + fee
	if fundingSatoshis < requiredSats {
		return nil, fmt.Errorf("insufficient funding: need %d sats, have %d", requiredSats, fundingSatoshis)
	}

	// Add change output if there's leftover
	change := fundingSatoshis - uint64(count) - uint64(estimatedSize)
	if change > 546 { // dust threshold
		if err := tx.PayToAddress(p.Address(), change); err != nil {
			return nil, fmt.Errorf("add change output: %w", err)
		}
	}

	// Sign the funding input
	if err := tx.Sign(); err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}

	// Broadcast
	success, failure := p.broadcaster.Broadcast(tx)
	if failure != nil {
		return nil, fmt.Errorf("broadcast failed: %s - %s", failure.Code, failure.Description)
	}

	txid := success.Txid

	// Get the locking script hex for the nonce UTXOs
	scriptHex, err := p.LockingScriptHex()
	if err != nil {
		return nil, fmt.Errorf("get locking script: %w", err)
	}

	// Store the minted nonce UTXOs
	p.mu.Lock()
	defer p.mu.Unlock()

	nonces := make([]NonceUTXO, count)
	for i := 0; i < count; i++ {
		n := NonceUTXO{
			TxID:     txid,
			Vout:     uint32(i),
			Script:   scriptHex,
			Satoshis: 1,
			Status:   StatusAvailable,
		}
		nonces[i] = n
		p.utxos[n.Outpoint()] = &nonces[i]
	}

	p.logger.Info("minted nonce UTXOs", "count", count, "txid", txid)
	return nonces, nil
}

// AddExisting adds pre-existing nonce UTXOs to the pool (e.g., loaded from persistence).
func (p *Pool) AddExisting(utxos []NonceUTXO) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range utxos {
		utxos[i].Status = StatusAvailable
		p.utxos[utxos[i].Outpoint()] = &utxos[i]
	}
}

// Lease finds an available nonce UTXO, marks it as leased, and returns it.
// Returns an error if no nonces are available.
func (p *Pool) Lease() (*NonceUTXO, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for _, n := range p.utxos {
		if n.Status == StatusAvailable {
			n.Status = StatusLeased
			n.LeasedAt = now
			n.ExpiresAt = now.Add(p.leaseTTL)
			return n, nil
		}
	}

	return nil, fmt.Errorf("no nonces available (pool exhausted)")
}

// MarkSpent marks a nonce UTXO as spent after successful delegation.
func (p *Pool) MarkSpent(txid string, vout uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := txid + ":" + itoa(vout)
	if n, ok := p.utxos[key]; ok {
		n.Status = StatusSpent
	}
}

// Lookup returns the nonce UTXO for a given outpoint, or nil if not found.
func (p *Pool) Lookup(txid string, vout uint32) *NonceUTXO {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := txid + ":" + itoa(vout)
	return p.utxos[key]
}

// Available returns the number of available nonce UTXOs.
func (p *Pool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, n := range p.utxos {
		if n.Status == StatusAvailable {
			count++
		}
	}
	return count
}

// Reclaim checks for expired leases and returns them to the available pool.
// Should be called periodically (e.g., every 30 seconds).
func (p *Pool) Reclaim() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	reclaimed := 0
	for _, n := range p.utxos {
		if n.Status == StatusLeased && now.After(n.ExpiresAt) {
			n.Status = StatusAvailable
			n.LeasedAt = time.Time{}
			n.ExpiresAt = time.Time{}
			reclaimed++
		}
	}

	if reclaimed > 0 {
		p.logger.Info("reclaimed expired nonce leases", "count", reclaimed)
	}
	return reclaimed
}

// StartReclaimLoop starts a background goroutine that periodically reclaims expired leases.
func (p *Pool) StartReclaimLoop(interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.Reclaim()
			case <-stop:
				return
			}
		}
	}()
}

// Stats returns pool statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	var stats PoolStats
	for _, n := range p.utxos {
		switch n.Status {
		case StatusAvailable:
			stats.Available++
		case StatusLeased:
			stats.Leased++
		case StatusSpent:
			stats.Spent++
		}
	}
	stats.Total = len(p.utxos)
	return stats
}

// PoolStats provides pool statistics.
type PoolStats struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Leased    int `json:"leased"`
	Spent     int `json:"spent"`
}
