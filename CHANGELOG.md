# Changelog

All notable changes to `github.com/ubgo/cache-obj` are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial release. `Cache[T]` contract — an in-process, zero-serialization, live-object-by-reference cache: `Get` / `Set` / `SetTTL` / `Del` / `Len` / `Purge` / `Stats`.
- `New[T]` with options: `WithCapacity` (LRU bound; non-positive = unbounded), `WithDefaultTTL`, `WithOnEvict`, `WithClock` (deterministic tests).
- `WithOnEvict` is **value-bearing**: the callback `func(key string, v T, cause cache.EvictionCause)` receives the evicted value (type inferred, no type parameter needed), so it can release resources the value owns (e.g. close a `*sql.DB`). Fires only on capacity (`cache.EvictSize`) and expiry (`cache.EvictExpired`) — never on `Del` / `Purge`.
- Lazy per-entry TTL expiry; `ttl <= 0` means no expiry.
- `Stats` reported via the shared `github.com/ubgo/cache.Stats` shape, including `EvictionsByCause`.
- `objtest.Run` conformance suite — the executable contract; the reference `New[T]` implementation passes it.
- Runnable godoc examples (`example_test.go`); README with the "different abstraction" positioning and when-not-to-use guidance.

### Documentation

- README "Use cases" section enumerating concrete live-object scenarios (compiled regex, templates, HTTP/gRPC/DB handles, per-key rate limiters, validators, live ORM entities, loaded models).
- README "Recipes" section: package-level singleton, cache-aside loader helper, caching a live ORM entity for edge traversal, periodic stats logging, releasing handles on eviction, per-key rate limiter, compiled template cache, and bounded-vs-unbounded.

### Notes

- This module is the family-branded successor to the now-deprecated `github.com/ubgo/threadsafecache`.
- Depends on `github.com/ubgo/cache` solely for the shared `Stats` / `EvictionCause` types, plus `hashicorp/golang-lru/v2` for storage.

### Open / under consideration

- **Single-flight `Remember`.** A get-or-load helper that dedupes concurrent misses on a hot key (the byte cache has `Remember`; the live-object cache does not yet).
