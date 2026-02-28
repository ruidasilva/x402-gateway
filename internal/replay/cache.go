package replay

import (
	"fmt"
	"sync"
	"time"
)

// Cache provides LRU-evicting, TTL-based replay detection.
// Key: nonce outpoint (txid:vout) → Value: the txid that settled it + challenge hash.
type Cache struct {
	mu      sync.RWMutex
	items   map[string]entry
	order   []string // for LRU eviction
	maxSize int
	ttl     time.Duration
}

type entry struct {
	spendTxID     string
	challengeHash string
	createdAt     time.Time
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

// outpointKey builds the cache key from a nonce outpoint.
func outpointKey(txid string, vout uint32) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}

// Check returns the spending txid and challenge hash if the nonce outpoint
// has already been settled. Returns ("", "", false) if not found or expired.
func (c *Cache) Check(txid string, vout uint32) (spendTxID, challengeHash string, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := outpointKey(txid, vout)
	e, ok := c.items[key]
	if !ok {
		return "", "", false
	}

	// Check TTL
	if time.Since(e.createdAt) > c.ttl {
		return "", "", false
	}

	return e.spendTxID, e.challengeHash, true
}

// Record stores a nonce outpoint → spending txid + challenge hash mapping.
func (c *Cache) Record(txid string, vout uint32, spendTxID, challengeHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	key := outpointKey(txid, vout)
	c.items[key] = entry{
		spendTxID:     spendTxID,
		challengeHash: challengeHash,
		createdAt:     time.Now(),
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
