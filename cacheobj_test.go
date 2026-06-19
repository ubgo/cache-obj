package cacheobj_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ubgo/cache"
	cacheobj "github.com/ubgo/cache-obj"
	"github.com/ubgo/cache-obj/objtest"
)

// boundedFactory builds a capacity-2 LRU cache (capacity 2 is what
// objtest's LRU-eviction check assumes).
func boundedFactory(opts ...cacheobj.Option) cacheobj.Cache[*objtest.Val] {
	return cacheobj.New[*objtest.Val](append([]cacheobj.Option{cacheobj.WithCapacity(2)}, opts...)...)
}

// unboundedFactory builds a cache with no capacity bound.
func unboundedFactory(opts ...cacheobj.Option) cacheobj.Cache[*objtest.Val] {
	return cacheobj.New[*objtest.Val](opts...)
}

func TestConformanceBounded(t *testing.T)   { objtest.Run(t, true, boundedFactory) }
func TestConformanceUnbounded(t *testing.T) { objtest.Run(t, false, unboundedFactory) }

// clock is a deterministic time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_000_000, 0)} }
func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// TestDefaultTTL exercises Set (not SetTTL) honoring WithDefaultTTL.
func TestDefaultTTL(t *testing.T) {
	clk := newClock()
	c := cacheobj.New[int](cacheobj.WithDefaultTTL(time.Hour), cacheobj.WithClock(clk.now))
	c.Set("k", 1)
	clk.advance(30 * time.Minute)
	if _, ok := c.Get("k"); !ok {
		t.Fatal("entry expired before default TTL")
	}
	clk.advance(time.Hour)
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry did not expire after default TTL")
	}
}

// TestWithClockNil covers the nil-clock reset path in New.
func TestWithClockNil(t *testing.T) {
	c := cacheobj.New[int](cacheobj.WithClock(nil))
	c.Set("k", 5)
	if v, ok := c.Get("k"); !ok || v != 5 {
		t.Fatalf("nil clock cache broken: %d, %v", v, ok)
	}
}

// TestExpiryFiresOnEvict covers evictExpired with a non-nil OnEvict, in both
// bounded and unbounded modes.
func TestExpiryFiresOnEvict(t *testing.T) {
	for _, tc := range []struct {
		name string
		cap  int
	}{{"unbounded", 0}, {"bounded", 4}} {
		t.Run(tc.name, func(t *testing.T) {
			clk := newClock()
			var got []cache.EvictionCause
			c := cacheobj.New[int](
				cacheobj.WithCapacity(tc.cap),
				cacheobj.WithClock(clk.now),
				cacheobj.WithOnEvict(func(_ string, cause cache.EvictionCause) {
					got = append(got, cause)
				}),
			)
			c.SetTTL("k", 1, time.Second)
			clk.advance(2 * time.Second)
			if _, ok := c.Get("k"); ok {
				t.Fatal("entry should have expired")
			}
			if len(got) != 1 || got[0] != cache.EvictExpired {
				t.Fatalf("OnEvict causes = %v, want [expired]", got)
			}
			if st := c.Stats(); st.EvictionsByCause[cache.EvictExpired] != 1 {
				t.Fatalf("expired eviction not counted: %+v", st)
			}
		})
	}
}

// TestStatsSnapshotIndependent ensures the returned EvictionsByCause map is a
// copy — mutating it must not corrupt internal state.
func TestStatsSnapshotIndependent(t *testing.T) {
	c := cacheobj.New[int](cacheobj.WithCapacity(1))
	c.Set("a", 1)
	c.Set("b", 2) // evicts a (size)
	st := c.Stats()
	st.EvictionsByCause[cache.EvictSize] = 999 // mutate the copy
	if again := c.Stats(); again.EvictionsByCause[cache.EvictSize] != 1 {
		t.Fatalf("internal byCause was mutated through the snapshot: %+v", again)
	}
}
