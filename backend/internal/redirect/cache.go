package redirect

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type Variant struct {
	ID        uuid.UUID
	TargetURL string
	Weight    int32
}

type Link struct {
	ID        uuid.UUID
	ClientID  uuid.UUID
	VideoID   uuid.NullUUID
	TargetURL string
	Active    bool
	ExpiresAt *time.Time
	Variants  []Variant
}

type item struct {
	link *Link // nil = known-missing (negative cache)
	exp  time.Time
}

// Cache is a small in-memory TTL cache keyed by slug. Swap for Redis when
// running multiple instances.
type Cache struct {
	mu     sync.RWMutex
	m      map[string]item
	ttl    time.Duration
	negTTL time.Duration
}

const maxEntries = 100_000

func NewCache(ttl, negTTL time.Duration) *Cache {
	return &Cache{m: make(map[string]item), ttl: ttl, negTTL: negTTL}
}

func (c *Cache) Get(slug string) (*Link, bool) {
	c.mu.RLock()
	it, ok := c.m[slug]
	c.mu.RUnlock()
	if !ok || time.Now().After(it.exp) {
		return nil, false
	}
	return it.link, true
}

func (c *Cache) Set(slug string, l *Link) {
	ttl := c.ttl
	if l == nil {
		ttl = c.negTTL
	}
	c.mu.Lock()
	if len(c.m) >= maxEntries { // crude bound against slug-scan attacks
		c.m = make(map[string]item)
	}
	c.m[slug] = item{link: l, exp: time.Now().Add(ttl)}
	c.mu.Unlock()
}

// Invalidate should be called by the link CRUD API after edits.
func (c *Cache) Invalidate(slug string) {
	c.mu.Lock()
	delete(c.m, slug)
	c.mu.Unlock()
}