// Package objtest is the conformance suite for the cacheobj contract. It IS
// the contract: any Cache[*Val] built by the supplied factory must pass
// Run, the same way cachetest.Run anchors the byte-cache family.
//
// A factory (not a finished cache) is taken so the suite can construct caches
// with its own deterministic clock and assorted options.
package objtest

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/cache"
	cacheobj "github.com/ubgo/cache-obj"
)

// Val is the reference value type the suite caches. It is a pointer target so
// the suite can assert reference identity (the defining behavior: Get returns
// the same object that was Set).
type Val struct{ N int }

// Factory builds a Cache[*Val] from the given options. Implementations pass
// cacheobj.New[*Val]. The suite appends its own WithClock so it controls time.
type Factory func(opts ...cacheobj.Option) cacheobj.Cache[*Val]

// Run executes the full conformance suite against caches built by f. Bounded
// indicates whether f produces a capacity-bounded cache (some checks — LRU
// eviction — only apply when bounded).
func Run(t *testing.T, bounded bool, f Factory) {
	t.Helper()

	t.Run("MissReturnsZeroFalse", func(t *testing.T) {
		c := f()
		if v, ok := c.Get("absent"); ok || v != nil {
			t.Fatalf("miss = (%v, %v), want (nil, false)", v, ok)
		}
	})

	t.Run("SetGetSameReference", func(t *testing.T) {
		c := f()
		want := &Val{N: 7}
		c.Set("k", want)
		got, ok := c.Get("k")
		if !ok {
			t.Fatal("Get after Set missed")
		}
		if got != want {
			t.Fatalf("Get returned a different reference: %p != %p", got, want)
		}
	})

	t.Run("Replace", func(t *testing.T) {
		c := f()
		c.Set("k", &Val{N: 1})
		c.Set("k", &Val{N: 2})
		got, ok := c.Get("k")
		if !ok || got.N != 2 {
			t.Fatalf("after replace = %v, %v; want N=2", got, ok)
		}
		if c.Len() != 1 {
			t.Fatalf("replace changed Len to %d, want 1", c.Len())
		}
	})

	t.Run("DelIsNoOpWhenAbsent", func(t *testing.T) {
		c := f()
		c.Del("never-there") // must not panic
		c.Set("k", &Val{N: 1})
		c.Del("k")
		if _, ok := c.Get("k"); ok {
			t.Fatal("Del did not remove the key")
		}
	})

	t.Run("Purge", func(t *testing.T) {
		c := f()
		c.Set("a", &Val{N: 1})
		c.Set("b", &Val{N: 2})
		c.Purge()
		if c.Len() != 0 {
			t.Fatalf("after Purge Len = %d, want 0", c.Len())
		}
		if _, ok := c.Get("a"); ok {
			t.Fatal("entry survived Purge")
		}
	})

	t.Run("ZeroTTLNeverExpires", func(t *testing.T) {
		clk := newClock()
		c := f(cacheobj.WithClock(clk.now))
		c.SetTTL("k", &Val{N: 1}, 0) // ttl <= 0 => no expiry
		clk.advance(1000 * time.Hour)
		if _, ok := c.Get("k"); !ok {
			t.Fatal("ttl<=0 entry expired, want immortal")
		}
	})

	t.Run("TTLExpiresAndEvicts", func(t *testing.T) {
		clk := newClock()
		c := f(cacheobj.WithClock(clk.now))
		c.SetTTL("k", &Val{N: 1}, 10*time.Second)

		clk.advance(5 * time.Second)
		if _, ok := c.Get("k"); !ok {
			t.Fatal("entry expired before its TTL")
		}
		clk.advance(10 * time.Second) // now past expiry
		if _, ok := c.Get("k"); ok {
			t.Fatal("entry did not expire after its TTL")
		}
		if c.Len() != 0 {
			t.Fatalf("expired entry not swept: Len = %d", c.Len())
		}
	})

	t.Run("StatsCountersAndHitRatio", func(t *testing.T) {
		c := f()
		c.Set("k", &Val{N: 1})
		c.Get("k")      // hit
		c.Get("absent") // miss
		c.Del("k")      // delete
		st := c.Stats()
		if st.Hits != 1 || st.Misses != 1 || st.Sets != 1 || st.Deletes != 1 {
			t.Fatalf("stats = %+v", st)
		}
		if hr := st.HitRatio(); hr != 0.5 {
			t.Fatalf("HitRatio = %v, want 0.5", hr)
		}
	})

	if bounded {
		t.Run("LRUEvictionFires", func(t *testing.T) {
			type ev struct {
				key string
				val *Val
			}
			var evicted []ev
			c := f(cacheobj.WithOnEvict(func(k string, v *Val, cause cache.EvictionCause) {
				if cause == cache.EvictSize {
					evicted = append(evicted, ev{k, v})
				}
			}))
			// Capacity is 2 by suite convention (see boundedFactory in tests).
			a := &Val{N: 1}
			c.Set("a", a)
			c.Set("b", &Val{N: 2})
			c.Set("c", &Val{N: 3}) // evicts "a" (LRU)
			if _, ok := c.Get("a"); ok {
				t.Fatal("a should have been LRU-evicted")
			}
			if len(evicted) != 1 || evicted[0].key != "a" {
				t.Fatalf("OnEvict size evictions = %v, want [a]", evicted)
			}
			if evicted[0].val != a {
				t.Fatal("OnEvict did not deliver the evicted value by reference")
			}
			st := c.Stats()
			if st.Evictions != 1 || st.EvictionsByCause[cache.EvictSize] != 1 {
				t.Fatalf("eviction stats = %+v", st)
			}
		})
	}

	t.Run("ConcurrentRace", func(_ *testing.T) {
		c := f()
		const workers, iters = 8, 300
		var wg sync.WaitGroup
		wg.Add(workers * 2)
		for w := 0; w < workers; w++ {
			go func() {
				defer wg.Done()
				for i := 0; i < iters; i++ {
					c.Set("k"+strconv.Itoa(i%32), &Val{N: i})
				}
			}()
			go func() {
				defer wg.Done()
				for i := 0; i < iters; i++ {
					_, _ = c.Get("k" + strconv.Itoa(i%32))
				}
			}()
		}
		wg.Wait()
	})
}

// clock is a deterministic, concurrency-safe time source for TTL tests.
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
