// Package cacheobj is the in-process, zero-serialization companion to
// github.com/ubgo/cache. It holds live Go objects by reference: what you Set
// is the exact value you Get back, with no codec, no copy, and no boxing.
//
// # When to use it
//
// Reach for cacheobj only when the value's usefulness depends on staying
// live — a *regexp.Regexp, an *http.Client, an open connection, a func, a
// chan, or any struct with unexported state that would not survive a codec
// round-trip (for example an ORM entity whose unexported client handle is
// nil after decoding). For serializable values (DTOs, configs, scalars,
// []Result) use github.com/ubgo/cache + cache-mem instead — it is strictly
// more capable there (single-flight, negative caching, redis/pg/tiered
// backends).
//
// # NOT a cache.Cache backend
//
// cacheobj is a DIFFERENT abstraction from github.com/ubgo/cache. The byte
// cache is []byte-in/[]byte-out so it can work uniformly over the network;
// cacheobj is typed (Cache[T]) and in-process only. It deliberately does not
// implement cache.Cache and cannot be swapped into a redis/pg slot. It reuses
// cache.Stats and cache.EvictionCause solely so observability reads
// identically across the family.
//
// # Mutation footgun
//
// Get returns the SAME reference that was Set — no defensive copy (that is
// the whole point, and impossible for non-copyable types anyway). Treat
// cached objects as immutable, or synchronize mutation yourself: a caller
// mutating a returned pointer mutates what every other caller sees.
//
// # Expiry
//
// Expiry is lazy: an entry past its TTL is detected and evicted on the Get
// that touches it; there is no background sweeper in this version. An LRU
// capacity bound (see WithCapacity) reclaims memory for keys that are never
// read again.
package cacheobj
