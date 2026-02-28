package replay

import (
	"sync"
	"time"
)

// Cache provides LRU-evicting, TTL-based replay detection.
// Key: challenge hash → Value: the txid that settled it.
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

// Check returns the spending txid if the challenge has already been settled.
// Returns ("", false) if not found.
func (c *Cache) Check(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

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

// Record stores a challenge hash → spending txid mapping.
func (c *Cache) Record(key string, spendTxID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

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
