// sweep.go — the optional background expiry sweeper + Close lifecycle
// (package cacheobj, github.com/ubgo/cache-obj).
//
// Without a sweeper, expiry is lazy: an entry past its TTL is only reclaimed
// on the Get that touches it, so an expired key that is never read again
// lingers until LRU capacity evicts it. WithSweepInterval starts a goroutine
// that periodically scans and evicts expired entries, bounding that memory.
// The cost is a goroutine that must be stopped with Close.

package cacheobj

import "time"

// sweepLoop runs until Close: on each tick it evicts expired entries.
func (s *Store[T]) sweepLoop(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.sweep()
		case <-s.done:
			return
		}
	}
}

// sweep removes every expired entry in one pass, firing OnEvict
// (cache.EvictExpired) for each. It holds the lock for the whole scan, so the
// interval should suit the cache size; keep OnEvict fast. Uses Peek so the
// scan does not disturb LRU recency.
func (s *Store[T]) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lru != nil {
		for _, key := range s.lru.Keys() {
			if e, ok := s.lru.Peek(key); ok && s.expired(e) {
				s.evictExpired(key, e.value)
			}
		}
		return
	}
	for key, e := range s.m {
		if s.expired(e) {
			s.evictExpired(key, e.value)
		}
	}
}

// Close stops the background sweeper started by WithSweepInterval. It is
// idempotent and safe to call even when no sweeper was started (a no-op). The
// cache remains usable after Close — only the background goroutine stops;
// expiry reverts to lazy (on Get).
func (s *Store[T]) Close() {
	s.closeOnce.Do(func() {
		if s.done != nil {
			close(s.done)
		}
	})
}
