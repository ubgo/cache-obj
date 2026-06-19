# Changelog

All notable changes to `github.com/ubgo/cache-obj` are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial release. `Cache[T]` contract — an in-process, zero-serialization, live-object-by-reference cache: `Get` / `Set` / `SetTTL` / `Del` / `Len` / `Purge` / `Stats`.
- `New[T]` with options: `WithCapacity` (LRU bound; non-positive = unbounded), `WithDefaultTTL`, `WithOnEvict` (fires on capacity + expiry evictions only), `WithClock` (deterministic tests).
- Lazy per-entry TTL expiry; `ttl <= 0` means no expiry.
- `Stats` reported via the shared `github.com/ubgo/cache.Stats` shape, including `EvictionsByCause`.
- `objtest.Run` conformance suite — the executable contract; the reference `New[T]` implementation passes it.
- Runnable godoc examples (`example_test.go`); README with the "different abstraction" positioning and when-not-to-use guidance.

### Documentation

- README "Recipes" section: package-level singleton, cache-aside loader helper, caching a live ORM entity for edge traversal, periodic stats logging, reacting to evictions, and bounded-vs-unbounded.

### Notes

- This module is the family-branded successor to the now-deprecated `github.com/ubgo/threadsafecache`.
- Depends on `github.com/ubgo/cache` solely for the shared `Stats` / `EvictionCause` types, plus `hashicorp/golang-lru/v2` for storage.

### Open / under consideration

- **Value-bearing eviction hook.** `WithOnEvict` is currently key-only (`func(key string, cause cache.EvictionCause)`), which cannot release resources owned by an evicted value (e.g. close a `*sql.DB`). A value-bearing variant — or making `OnEvict` carry the value — is under consideration for callers that cache resource-owning objects.
- **Single-flight `Remember`.** A get-or-load helper that dedupes concurrent misses on a hot key (the byte cache has `Remember`; the live-object cache does not yet).
