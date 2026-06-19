package cacheobj_test

import (
	"testing"
	"time"

	"github.com/ubgo/cache"
	cacheobj "github.com/ubgo/cache-obj"
)

// sweepEvictsExpired runs the background sweeper against a cache built by
// newCache and asserts an expired key is reclaimed by the goroutine (not by a
// Get), while a no-TTL key is left untouched (exercises the not-expired
// branch of the scan).
func sweepEvictsExpired(t *testing.T, newCache func(onEvict cacheobj.Option) *cacheobj.Store[int]) {
	t.Helper()
	evicted := make(chan string, 8)
	c := newCache(cacheobj.WithOnEvict(func(key string, _ int, cause cache.EvictionCause) {
		if cause == cache.EvictExpired {
			evicted <- key
		}
	}))
	defer c.Close()

	c.SetTTL("immortal", 1, 0)              // never expires — sweep must keep it
	c.SetTTL("temp", 2, 5*time.Millisecond) // expires soon — sweep must drop it

	select {
	case key := <-evicted:
		if key != "temp" {
			t.Fatalf("sweeper evicted %q, want \"temp\"", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background sweeper did not evict the expired entry")
	}

	if _, ok := c.Get("immortal"); !ok {
		t.Fatal("sweeper evicted the no-TTL entry")
	}
}

func TestSweeperUnbounded(t *testing.T) {
	sweepEvictsExpired(t, func(onEvict cacheobj.Option) *cacheobj.Store[int] {
		return cacheobj.New[int](cacheobj.WithSweepInterval(time.Millisecond), onEvict)
	})
}

func TestSweeperBounded(t *testing.T) {
	sweepEvictsExpired(t, func(onEvict cacheobj.Option) *cacheobj.Store[int] {
		return cacheobj.New[int](
			cacheobj.WithCapacity(16),
			cacheobj.WithSweepInterval(time.Millisecond),
			onEvict,
		)
	})
}

func TestCloseIdempotent(_ *testing.T) {
	c := cacheobj.New[int](cacheobj.WithSweepInterval(time.Millisecond))
	c.Close()
	c.Close() // must not panic (closeOnce guards the channel)
}

func TestCloseNoSweeperIsNoOp(t *testing.T) {
	c := cacheobj.New[int]() // no sweeper started
	c.Close()                // done == nil → harmless no-op
	// Cache still works after Close.
	c.Set("k", 1)
	if v, ok := c.Get("k"); !ok || v != 1 {
		t.Fatalf("cache broken after Close: %d, %v", v, ok)
	}
}
