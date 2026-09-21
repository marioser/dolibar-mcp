package doldb

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// cachingDB builds a DB whose cache is live but whose connection is never
// used: cachedRead is the seam every read goes through, so it can be exercised
// without a database.
func cachingDB(t *testing.T, ttl time.Duration) *DB {
	t.Helper()
	cfg := unreachableConfig(t)
	cfg.CacheTTL = ttl
	cfg.CacheMaxEntries = 100

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCachedReadHitsTheDatabaseOnce(t *testing.T) {
	db := cachingDB(t, time.Minute)

	var loads int32
	load := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		return "proposal", nil
	}

	for i := 0; i < 5; i++ {
		got, err := db.cachedRead("k", load)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if got != "proposal" {
			t.Fatalf("read %d returned %v", i, got)
		}
	}

	if n := atomic.LoadInt32(&loads); n != 1 {
		t.Fatalf("hit the database %d times for five identical reads, want 1", n)
	}
}

func TestCachedReadDoesNotCacheErrors(t *testing.T) {
	// A failed read says nothing about the data. Caching it would turn one
	// blip into thirty seconds of manufactured failures.
	db := cachingDB(t, time.Minute)

	want := errors.New("connection lost")
	var loads int32
	load := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		return nil, want
	}

	for i := 0; i < 3; i++ {
		if _, err := db.cachedRead("k", load); !errors.Is(err, want) {
			t.Fatalf("read %d: err = %v, want %v", i, err, want)
		}
	}

	if n := atomic.LoadInt32(&loads); n != 3 {
		t.Fatalf("load ran %d times, want 3 — an error was cached", n)
	}
}

func TestCachedReadIsBypassedWhenDisabled(t *testing.T) {
	db := cachingDB(t, 0)

	var loads int32
	load := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		return "x", nil
	}

	db.cachedRead("k", load)
	db.cachedRead("k", load)

	if n := atomic.LoadInt32(&loads); n != 2 {
		t.Fatalf("load ran %d times with the cache off, want 2", n)
	}
}

func TestInvalidateReadsForcesTheNextReadToTheDatabase(t *testing.T) {
	db := cachingDB(t, time.Minute)

	var loads int32
	load := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		return "x", nil
	}

	db.cachedRead("k", load)
	db.InvalidateReads() // a write landed
	db.cachedRead("k", load)

	if n := atomic.LoadInt32(&loads); n != 2 {
		t.Fatalf("load ran %d times, want 2 — the write did not clear the cache", n)
	}
}

func TestReadInFlightDuringAWriteIsNotCached(t *testing.T) {
	// The read starts, a write lands, the read finishes. Its result is a
	// pre-write snapshot: the caller may have it, but it must not be stored.
	db := cachingDB(t, time.Minute)

	var loads int32
	slowLoad := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		db.InvalidateReads() // the write lands mid-read
		return "pre-write", nil
	}

	if _, err := db.cachedRead("k", slowLoad); err != nil {
		t.Fatalf("first read: %v", err)
	}

	db.cachedRead("k", func() (any, error) {
		atomic.AddInt32(&loads, 1)
		return "post-write", nil
	})

	if n := atomic.LoadInt32(&loads); n != 2 {
		t.Fatalf("load ran %d times, want 2 — the pre-write snapshot was cached", n)
	}
}

func TestConcurrentIdenticalReadsCollapseIntoOne(t *testing.T) {
	// Duplicate suppression is the part of this that cannot go stale: the
	// callers are all asking the same question at the same instant.
	db := cachingDB(t, time.Minute)

	release := make(chan struct{})
	var loads int32
	load := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		<-release // hold the leader so the others pile up behind it
		return "x", nil
	}

	const callers = 16
	var wg sync.WaitGroup
	var started sync.WaitGroup
	started.Add(callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.Done()
			db.cachedRead("k", load)
		}()
	}

	started.Wait()
	time.Sleep(50 * time.Millisecond) // let every caller reach the group
	close(release)
	wg.Wait()

	if n := atomic.LoadInt32(&loads); n > 2 {
		t.Fatalf("%d callers produced %d database round trips, want 1 (2 tolerated for scheduling)", callers, n)
	}
}

func TestDifferentKeysAreNotCollapsed(t *testing.T) {
	db := cachingDB(t, time.Minute)

	var loads int32
	load := func() (any, error) {
		atomic.AddInt32(&loads, 1)
		return "x", nil
	}

	db.cachedRead("a", load)
	db.cachedRead("b", load)

	if n := atomic.LoadInt32(&loads); n != 2 {
		t.Fatalf("two different keys produced %d round trips, want 2", n)
	}
}

func TestCacheStatsReportUsage(t *testing.T) {
	db := cachingDB(t, time.Minute)

	load := func() (any, error) { return "x", nil }
	db.cachedRead("k", load) // miss
	db.cachedRead("k", load) // hit

	s := db.CacheStats()
	if !s.Enabled {
		t.Error("stats report the cache as disabled")
	}
	if s.Hits != 1 || s.Misses != 1 {
		t.Errorf("stats = %+v, want 1 hit and 1 miss", s)
	}
	if s.Entries != 1 {
		t.Errorf("entries = %d, want 1", s.Entries)
	}
	if s.HitRate != 0.5 {
		t.Errorf("hit rate = %v, want 0.5", s.HitRate)
	}
}
