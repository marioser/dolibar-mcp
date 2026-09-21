package doldb

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// readCache is a small, short-lived, whole-cache-invalidating store for read
// results.
//
// Two things make a cache appropriate here despite this being an ERP. First,
// the caller is an LLM: it re-reads the same proposal or re-runs the same
// search several times while it reasons, so the duplicate load is real and
// entirely wasted. Second, every write in this server goes out through the
// REST API in internal/dolapi, which gives us one choke point to invalidate
// from — so a cached read can never outlive a change this server made.
//
// The TTL is deliberately short and the invalidation deliberately coarse. A
// stale number in a quotation is worse than a slow query, so correctness wins
// over hit rate every time.
type readCache struct {
	ttl time.Duration
	max int

	mu      sync.Mutex
	entries map[string]cacheEntry
	// gen is bumped on every invalidation so a read that started before a
	// write cannot publish its pre-write snapshot afterwards.
	gen    uint64
	hits   uint64
	misses uint64
}

type cacheEntry struct {
	value   any
	expires time.Time
}

// CacheStats is a snapshot of cache behaviour, surfaced on /health so the
// cache can be judged from outside instead of guessed at.
type CacheStats struct {
	Enabled bool    `json:"enabled"`
	TTL     string  `json:"ttl"`
	Entries int     `json:"entries"`
	Max     int     `json:"max_entries"`
	Hits    uint64  `json:"hits"`
	Misses  uint64  `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

func newReadCache(ttl time.Duration, max int) *readCache {
	if max <= 0 {
		max = 1
	}
	return &readCache{
		ttl:     ttl,
		max:     max,
		entries: make(map[string]cacheEntry),
	}
}

func (c *readCache) disabled() bool { return c == nil || c.ttl <= 0 }

func (c *readCache) get(key string) (any, bool) {
	if c.disabled() {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		if ok {
			delete(c.entries, key)
		}
		c.misses++
		return nil, false
	}
	c.hits++
	return e.value, true
}

// generation reads the invalidation counter. A caller takes it before it
// starts a read and hands it back to putIfCurrent, so a write that lands in
// between discards the result instead of caching data that is already old.
func (c *readCache) generation() uint64 {
	if c.disabled() {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *readCache) putIfCurrent(key string, value any, gen uint64) {
	if c.disabled() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return
	}
	c.store(key, value)
}

// store assumes c.mu is held.
func (c *readCache) store(key string, value any) {
	if len(c.entries) >= c.max {
		c.evict()
	}
	c.entries[key] = cacheEntry{value: value, expires: time.Now().Add(c.ttl)}
}

// evict assumes c.mu is held. Expired entries go first; if that frees nothing,
// the entry closest to expiring is dropped. At these sizes (hundreds of
// entries) a linear scan is cheaper than maintaining an LRU list.
func (c *readCache) evict() {
	now := time.Now()
	freed := false
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
			freed = true
		}
	}
	if freed && len(c.entries) < c.max {
		return
	}

	var oldestKey string
	var oldest time.Time
	for k, e := range c.entries {
		if oldestKey == "" || e.expires.Before(oldest) {
			oldestKey, oldest = k, e.expires
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// invalidate drops every entry. It is called after any successful write, so
// the next read goes back to the database.
func (c *readCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.entries = make(map[string]cacheEntry)
}

func (c *readCache) len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *readCache) stats() CacheStats {
	if c == nil {
		return CacheStats{Enabled: false, TTL: "0s"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	s := CacheStats{
		Enabled: c.ttl > 0,
		TTL:     c.ttl.String(),
		Entries: len(c.entries),
		Max:     c.max,
		Hits:    c.hits,
		Misses:  c.misses,
	}
	if total := s.Hits + s.Misses; total > 0 {
		s.HitRate = float64(s.Hits) / float64(total)
	}
	return s
}

// fetchKey identifies a single-entity read. An id lookup and a ref lookup are
// kept apart even when they resolve to the same record: answering one with the
// other would depend on a resolution this cache never performed.
func fetchKey(entity string, id int64, ref string) string {
	return "fetch\x00" + entity + "\x00" + fmt.Sprint(id) + "\x00" + ref
}

// searchKey identifies a search. Every field of SearchParams takes part.
// The optional filters are dereferenced rather than printed: fmt would render
// a *int as its address, which changes on every call and would make the key
// both unstable and meaningless.
func searchKey(p SearchParams) string {
	var b strings.Builder
	b.WriteString("search")

	write := func(v any) {
		b.WriteByte(0)
		fmt.Fprint(&b, v)
	}

	write(p.Entity)
	write(p.Query)
	write(p.CustomerID)
	writeOptional(&b, p.Status)
	write(p.DateFrom)
	write(p.DateTo)
	writeOptional(&b, p.AmountMin)
	writeOptional(&b, p.AmountMax)
	write(p.Limit)
	write(p.Offset)

	return b.String()
}

// writeOptional renders a nullable filter by value. An unset filter and a
// filter set to the zero value are different searches, so they get different
// markers.
func writeOptional[T any](b *strings.Builder, v *T) {
	b.WriteByte(0)
	if v == nil {
		b.WriteString("~")
		return
	}
	fmt.Fprint(b, *v)
}
