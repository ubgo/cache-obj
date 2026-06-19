// store.go — the in-memory Cache[T] implementation: LRU bound + per-entry
// TTL + lazy expiry + Stats, behind one sync.Mutex (package cacheobj,
// github.com/ubgo/cache-obj).
//
// One lock guards everything. hashicorp/golang-lru mutates internal recency
// state on Get, so a read-lock fast path is unsafe; a single Mutex keeps the
// implementation simple and race-clean. Counters live under the same lock,
// so no atomics are needed.
//
// Eviction-cause discipline: golang-lru's eviction callback fires for Add
// (capacity), Remove, and Purge alike — it cannot tell us the cause. We use a
// `suppress` flag (set under the lock) to silence the callback during the
// removals we trigger deliberately, so the only callback that reaches the
// user's OnEvict hook is a genuine capacity eviction. Expiry evictions are
// reported explicitly. Del/Purge are deliberate, so they fire nothing.

package cacheobj

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ubgo/cache"
)

// Option configures a Store at construction. The option type is non-generic:
// every knob (capacity, TTL, clock, OnEvict) is independent of the value type
// T, so options compose cleanly with New[T].
type Option func(*config)

type config struct {
	capacity int
	ttl      time.Duration
	now      func() time.Time
	onEvict  func(key string, cause cache.EvictionCause)
}

// WithCapacity bounds the cache to at most n entries using LRU eviction. A
// non-positive n (the default) means unbounded — entries are removed only by
// TTL expiry, Del, or Purge.
func WithCapacity(n int) Option { return func(c *config) { c.capacity = n } }

// WithDefaultTTL sets the TTL applied by Set. A non-positive d (the default)
// means Set-stored entries never expire. SetTTL always overrides this.
func WithDefaultTTL(d time.Duration) Option { return func(c *config) { c.ttl = d } }

// WithOnEvict registers a callback invoked when an entry is dropped
// involuntarily: by capacity (cause cache.EvictSize) or TTL expiry (cause
// cache.EvictExpired). It is NOT called for Del or Purge. The callback runs
// while the cache lock is held — keep it fast and do not call back into the
// cache from it.
func WithOnEvict(fn func(key string, cause cache.EvictionCause)) Option {
	return func(c *config) { c.onEvict = fn }
}

// WithClock overrides the time source, for deterministic TTL tests. Defaults
// to time.Now.
func WithClock(now func() time.Time) Option { return func(c *config) { c.now = now } }

// Store is the in-memory Cache[T] implementation returned by New.
type Store[T any] struct {
	mu  sync.Mutex
	cfg config

	// Exactly one of lru / m is non-nil: lru when bounded, m when unbounded.
	lru *lru.Cache[string, entry[T]]
	m   map[string]entry[T]

	// suppress silences onLRUEvict during deliberate removals (see file doc).
	suppress bool

	hits, misses, sets, deletes, evictions int64
	byCause                                map[cache.EvictionCause]int64
}

// Compile-time assertion that *Store[T] satisfies the Cache[T] contract.
var _ Cache[int] = (*Store[int])(nil)

// New builds an in-memory live-object cache. With no options it is unbounded
// with no default TTL. See the WithX options for capacity, TTL, eviction
// hook, and clock.
func New[T any](opts ...Option) *Store[T] {
	cfg := config{now: time.Now}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	s := &Store[T]{
		cfg:     cfg,
		byCause: make(map[cache.EvictionCause]int64),
	}
	if cfg.capacity > 0 {
		// NewWithEvict only errors on size <= 0, which cfg.capacity > 0 rules
		// out; the returned error is therefore always nil here.
		l, _ := lru.NewWithEvict[string, entry[T]](cfg.capacity, s.onLRUEvict)
		s.lru = l
	} else {
		s.m = make(map[string]entry[T])
	}
	return s
}

// Get implements Cache.
func (s *Store[T]) Get(key string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.load(key)
	if !ok {
		s.misses++
		var zero T
		return zero, false
	}
	if s.expired(e) {
		s.evictExpired(key)
		s.misses++
		var zero T
		return zero, false
	}
	s.hits++
	return e.value, true
}

// Set implements Cache.
func (s *Store[T]) Set(key string, v T) { s.SetTTL(key, v, s.cfg.ttl) }

// SetTTL implements Cache.
func (s *Store[T]) SetTTL(key string, v T, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exp time.Time
	if ttl > 0 {
		exp = s.cfg.now().Add(ttl)
	}
	s.add(key, entry[T]{value: v, expiresAt: exp})
	s.sets++
}

// Del implements Cache.
func (s *Store[T]) Del(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remove(key)
	s.deletes++
}

// Len implements Cache.
func (s *Store[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lru != nil {
		return s.lru.Len()
	}
	return len(s.m)
}

// Purge implements Cache.
func (s *Store[T]) Purge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lru != nil {
		s.suppress = true
		s.lru.Purge()
		s.suppress = false
		return
	}
	s.m = make(map[string]entry[T])
}

// Stats implements Cache.
func (s *Store[T]) Stats() cache.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()

	byCause := make(map[cache.EvictionCause]int64, len(s.byCause))
	for k, v := range s.byCause {
		byCause[k] = v
	}
	var entries int64
	if s.lru != nil {
		entries = int64(s.lru.Len())
	} else {
		entries = int64(len(s.m))
	}
	return cache.Stats{
		Hits:             s.hits,
		Misses:           s.misses,
		Sets:             s.sets,
		Deletes:          s.deletes,
		Evictions:        s.evictions,
		EvictionsByCause: byCause,
		Entries:          entries,
	}
}

// --- internals (all called with s.mu held) ---

func (s *Store[T]) load(key string) (entry[T], bool) {
	if s.lru != nil {
		return s.lru.Get(key) // also refreshes LRU recency
	}
	e, ok := s.m[key]
	return e, ok
}

func (s *Store[T]) add(key string, e entry[T]) {
	if s.lru == nil {
		s.m[key] = e
		return
	}
	// Replacing an existing key fires the eviction callback for the old
	// value; that is not a capacity eviction, so suppress it. Adding a new
	// key at capacity legitimately evicts the LRU tail — leave the callback
	// live for that.
	if s.lru.Contains(key) {
		s.suppress = true
		s.lru.Add(key, e)
		s.suppress = false
		return
	}
	s.lru.Add(key, e)
}

func (s *Store[T]) remove(key string) {
	if s.lru != nil {
		s.suppress = true
		s.lru.Remove(key)
		s.suppress = false
		return
	}
	delete(s.m, key)
}

func (s *Store[T]) evictExpired(key string) {
	s.remove(key)
	s.evictions++
	s.byCause[cache.EvictExpired]++
	if s.cfg.onEvict != nil {
		s.cfg.onEvict(key, cache.EvictExpired)
	}
}

// onLRUEvict is golang-lru's eviction callback. It only counts/reports
// genuine capacity evictions; deliberate removals set s.suppress first.
func (s *Store[T]) onLRUEvict(key string, _ entry[T]) {
	if s.suppress {
		return
	}
	s.evictions++
	s.byCause[cache.EvictSize]++
	if s.cfg.onEvict != nil {
		s.cfg.onEvict(key, cache.EvictSize)
	}
}

func (s *Store[T]) expired(e entry[T]) bool {
	if e.expiresAt.IsZero() {
		return false
	}
	return !s.cfg.now().Before(e.expiresAt) // now >= expiresAt
}
