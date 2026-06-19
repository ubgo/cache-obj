// remember.go — single-flight get-or-load on top of the live-object store
// (package cacheobj, github.com/ubgo/cache-obj).
//
// Remember collapses a thundering herd: when N goroutines miss the same cold
// key at once, the loader runs ONCE and the other N-1 callers wait and share
// that result. The dedup state (loaderFlight) lives on the Store so it is
// scoped per cache instance and typed on T — no boxing, no global registry.

package cacheobj

import (
	"sync"
	"time"
)

// loaderFlight is a per-key single-flight group. The first caller for a key
// becomes the leader and runs the loader; concurrent callers for the same key
// block on the leader's WaitGroup and share its result. The map entry is
// removed once the leader finishes, so the next cold miss starts a fresh load.
type loaderFlight[T any] struct {
	mu sync.Mutex
	m  map[string]*flightCall[T]
}

type flightCall[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
}

func (g *loaderFlight[T]) do(key string, fn func() (T, error)) (T, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall[T])
	}
	if c, ok := g.m[key]; ok {
		// A load for this key is already in flight: wait for it and share
		// the leader's result instead of running the loader again.
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &flightCall[T]{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	// Leader: run the loader exactly once. defer guarantees waiters are
	// released and the entry is cleaned up even if fn panics.
	defer func() {
		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		c.wg.Done()
	}()
	c.val, c.err = fn()
	return c.val, c.err
}

// Remember returns the cached value for key, or loads it via fn and stores it
// with the given ttl (ttl <= 0 means no expiry). Under concurrent misses for
// the same key, fn runs exactly once (single-flight) and every caller shares
// the result.
//
// Loader errors are returned to all waiting callers and are NOT cached — the
// next call retries. Loaded values are stored by reference, like Set.
//
// fn must not call Remember for the same key (it would deadlock waiting on
// itself), and should not panic (a panic propagates to the leader; waiters are
// released but observe the zero value).
func (s *Store[T]) Remember(key string, ttl time.Duration, fn func() (T, error)) (T, error) {
	if v, ok := s.Get(key); ok {
		return v, nil
	}
	return s.flight.do(key, func() (T, error) {
		v, err := fn()
		if err != nil {
			var zero T
			return zero, err
		}
		s.SetTTL(key, v, ttl)
		return v, nil
	})
}
