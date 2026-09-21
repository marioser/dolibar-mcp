package doldb

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// put is the unguarded store used by the tests. Production always goes through
// putIfCurrent so a write landing mid-read cannot publish a stale snapshot.
func put(c *readCache, key string, value any) {
	c.putIfCurrent(key, value, c.generation())
}

func TestCacheReturnsWhatItStored(t *testing.T) {
	c := newReadCache(time.Minute, 10)
	put(c, "k", "value")

	got, ok := c.get("k")
	if !ok {
		t.Fatal("miss on a key that was just stored")
	}
	if got != "value" {
		t.Fatalf("got %v, want %q", got, "value")
	}
}

func TestCacheMissesUnknownKey(t *testing.T) {
	c := newReadCache(time.Minute, 10)
	if _, ok := c.get("nope"); ok {
		t.Fatal("hit on a key that was never stored")
	}
}

func TestCacheExpiresEntries(t *testing.T) {
	c := newReadCache(20*time.Millisecond, 10)
	put(c, "k", "value")

	time.Sleep(40 * time.Millisecond)

	if _, ok := c.get("k"); ok {
		t.Fatal("hit on an entry past its TTL — stale ERP data is worse than a slow query")
	}
}

func TestDisabledCacheNeverStores(t *testing.T) {
	// TTL 0 is the documented kill switch.
	c := newReadCache(0, 10)
	put(c, "k", "value")

	if _, ok := c.get("k"); ok {
		t.Fatal("a disabled cache served an entry")
	}
	if !c.disabled() {
		t.Fatal("disabled() = false for a zero TTL")
	}
}

func TestInvalidateDropsEverything(t *testing.T) {
	c := newReadCache(time.Minute, 10)
	put(c, "a", 1)
	put(c, "b", 2)

	c.invalidate()

	if _, ok := c.get("a"); ok {
		t.Error("key a survived invalidation")
	}
	if _, ok := c.get("b"); ok {
		t.Error("key b survived invalidation")
	}
}

func TestInvalidateDropsReadsAlreadyInFlight(t *testing.T) {
	// A write lands while a read is still running. The read must not be able to
	// publish the pre-write snapshot it already fetched.
	c := newReadCache(time.Minute, 10)

	gen := c.generation()
	c.invalidate()
	c.putIfCurrent("k", "stale", gen)

	if _, ok := c.get("k"); ok {
		t.Fatal("a read that started before the write published stale data")
	}
}

func TestPutIfCurrentStoresWhenNoWriteIntervened(t *testing.T) {
	c := newReadCache(time.Minute, 10)

	gen := c.generation()
	c.putIfCurrent("k", "fresh", gen)

	got, ok := c.get("k")
	if !ok || got != "fresh" {
		t.Fatalf("got (%v, %v), want (fresh, true)", got, ok)
	}
}

func TestCacheEvictsWhenFull(t *testing.T) {
	const max = 4
	c := newReadCache(time.Minute, max)

	for i := 0; i < max*3; i++ {
		put(c, string(rune('a'+i)), i)
	}

	if n := c.len(); n > max {
		t.Fatalf("cache holds %d entries, want at most %d", n, max)
	}
}

func TestCacheTracksHitsAndMisses(t *testing.T) {
	c := newReadCache(time.Minute, 10)
	put(c, "k", 1)

	c.get("k")     // hit
	c.get("k")     // hit
	c.get("other") // miss

	s := c.stats()
	if s.Hits != 2 || s.Misses != 1 {
		t.Fatalf("stats = %+v, want 2 hits and 1 miss", s)
	}
}

func TestCacheIsSafeUnderConcurrency(t *testing.T) {
	c := newReadCache(time.Minute, 50)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i%10))
			put(c, key, i)
			c.get(key)
			if i%8 == 0 {
				c.invalidate()
			}
			c.stats()
		}(i)
	}
	wg.Wait()
}

func TestCacheKeysDistinguishParameters(t *testing.T) {
	// Two searches that differ only in offset must not share an entry.
	a := searchKey(SearchParams{Entity: "proposals", Limit: 25, Offset: 0})
	b := searchKey(SearchParams{Entity: "proposals", Limit: 25, Offset: 25})
	if a == b {
		t.Fatalf("offset 0 and 25 share the key %q", a)
	}

	c := searchKey(SearchParams{Entity: "proposals", Query: "acme"})
	d := searchKey(SearchParams{Entity: "proposals", Query: "acme corp"})
	if c == d {
		t.Fatalf("different queries share the key %q", c)
	}

	if searchKey(SearchParams{Entity: "orders"}) == searchKey(SearchParams{Entity: "proposals"}) {
		t.Fatal("different entities share a key")
	}
}

func TestCacheKeysDereferenceOptionalFilters(t *testing.T) {
	// fmt renders a *int as its address, which changes on every call. A key
	// built that way is never reused and leaks a pointer into the map.
	one, two := 1, 2
	a := searchKey(SearchParams{Entity: "proposals", Status: &one})
	b := searchKey(SearchParams{Entity: "proposals", Status: &two})
	if a == b {
		t.Fatal("status 1 and 2 share a key")
	}
	if strings.Contains(a, "0x") {
		t.Fatalf("key %q contains a pointer address", a)
	}

	// Same value, different pointer: must be the same key.
	alsoOne := 1
	if searchKey(SearchParams{Entity: "proposals", Status: &one}) !=
		searchKey(SearchParams{Entity: "proposals", Status: &alsoOne}) {
		t.Fatal("two pointers to the same value produced different keys")
	}

	// Unset is not the same search as set-to-zero.
	zero := 0
	if searchKey(SearchParams{Entity: "proposals"}) ==
		searchKey(SearchParams{Entity: "proposals", Status: &zero}) {
		t.Fatal("an unset filter shares a key with status=0")
	}

	min, max := 10.0, 20.0
	if searchKey(SearchParams{Entity: "proposals", AmountMin: &min}) ==
		searchKey(SearchParams{Entity: "proposals", AmountMax: &max}) {
		t.Fatal("amount_min and amount_max share a key")
	}
}

func TestCacheKeysAreStableForTheSameParameters(t *testing.T) {
	p := SearchParams{Entity: "proposals", Query: "acme", CustomerID: 7, Limit: 25}
	if searchKey(p) != searchKey(p) {
		t.Fatal("the same parameters produced two different keys")
	}
}

func TestFetchKeysDistinguishIdentityAndEntity(t *testing.T) {
	if fetchKey("proposals", 1, "") == fetchKey("proposals", 2, "") {
		t.Error("different ids share a key")
	}
	if fetchKey("proposals", 1, "") == fetchKey("orders", 1, "") {
		t.Error("different entities share a key")
	}
	if fetchKey("proposals", 0, "PR2601") == fetchKey("proposals", 0, "PR2602") {
		t.Error("different refs share a key")
	}
	// An id lookup and a ref lookup are different queries even when they
	// resolve to the same record — never let one answer the other blindly.
	if fetchKey("proposals", 1, "") == fetchKey("proposals", 0, "PR2601") {
		t.Error("id and ref lookups share a key")
	}
}
