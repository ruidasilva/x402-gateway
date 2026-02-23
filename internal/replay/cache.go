package replay

import (
	"fmt"
	"sync"
	"time"
)

// Cache provides LRU-evicting, TTL-based replay detection for nonce outpoints.
// Key: "txid:vout" of the nonce UTXO → Value: the txid that spent it.
type Cache struct {
	mu      sync.RWMutex
	items   map[string]entry
	order   []string // for LRU eviction
	maxSize int
	ttl     time.Duration
}

type entry struct {
	spendTxID string
	createdAt time.Time
}

// New creates a replay cache with the given TTL and max size.
func New(ttl time.Duration, maxSize int) *Cache {
	return &Cache{
		items:   make(map[string]entry, maxSize),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Check returns the spending txid if the nonce outpoint has already been spent.
// Returns ("", false) if not found.
func (c *Cache) Check(nonceTxID string, nonceVout uint32) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := outpointKey(nonceTxID, nonceVout)
	e, ok := c.items[key]
	if !ok {
		return "", false
	}

	// Check TTL
	if time.Since(e.createdAt) > c.ttl {
		return "", false
	}

	return e.spendTxID, true
}

// Record stores a nonce outpoint → spending txid mapping.
func (c *Cache) Record(nonceTxID string, nonceVout uint32, spendTxID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := outpointKey(nonceTxID, nonceVout)

	// Evict if at capacity
	if len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	c.items[key] = entry{
		spendTxID: spendTxID,
		createdAt: time.Now(),
	}
	c.order = append(c.order, key)
}

// Cleanup removes expired entries. Call periodically.
func (c *Cache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	now := time.Now()
	newOrder := make([]string, 0, len(c.order))
	for _, key := range c.order {
		e, ok := c.items[key]
		if !ok {
			continue
		}
		if now.Sub(e.createdAt) > c.ttl {
			delete(c.items, key)
			removed++
		} else {
			newOrder = append(newOrder, key)
		}
	}
	c.order = newOrder
	return removed
}

// Size returns the number of entries in the cache.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *Cache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.items, oldest)
}

func outpointKey(txid string, vout uint32) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}
