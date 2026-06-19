# cache-obj

> The in-process, zero-serialization companion to [`github.com/ubgo/cache`](https://github.com/ubgo/cache). Holds **live Go objects by reference** — what you `Set` is the exact instance you `Get` back, with no codec, no copy, no boxing.

```go
import cacheobj "github.com/ubgo/cache-obj"

// Cache compiled regexes — a value that must stay live (it can't survive a codec).
re := cacheobj.New[*regexp.Regexp](cacheobj.WithCapacity(128))

re.Set(`\d+`, regexp.MustCompile(`\d+`))

if r, ok := re.Get(`\d+`); ok {
    r.MatchString("abc123") // same compiled program, zero cost
}
```

> [!IMPORTANT]
> **`cache-obj` is NOT a `cache.Cache` backend.** It is a *different abstraction* from `github.com/ubgo/cache`: typed (`Cache[T]`), in-process only, no serialization. It deliberately does not implement `cache.Cache` and cannot be swapped into a redis/pg slot. For **serializable** values (DTOs, configs, scalars, `[]Result`) use `ubgo/cache` + `cache-mem` — it is strictly more capable there (single-flight, negative caching, network backends). Reach for `cache-obj` **only** when the value must stay live.

## When to use which

| You want to cache… | Use |
|---|---|
| A DTO, config, scalar, or any serializable value | `ubgo/cache` + `cache-mem` |
| The same value over redis / pg / tiered | `ubgo/cache` + a network backend |
| A `*regexp.Regexp`, `*http.Client`, open connection, `func`, `chan` | **`cache-obj`** |
| An object with unexported state that must survive intact (e.g. an ORM entity you traverse/mutate) | **`cache-obj`** |

The dividing question: **after `Get`, do you need the *original object*, or just its *data*?** Original object (liveness) → `cache-obj`. Just the data → `ubgo/cache`.

## Install

```sh
go get github.com/ubgo/cache-obj
```

Requires Go 1.24+. One third-party dependency (`hashicorp/golang-lru/v2`) plus `github.com/ubgo/cache` (imported solely for the shared `Stats` / `EvictionCause` types).

## What you get

- **Live objects, by reference.** `Get` returns the same instance you `Set` — no serialization, no copy. The only cache in the family that can hold non-serializable values.
- **Generics.** `Cache[T]` stores `T` directly, no `interface{}` boxing.
- **Per-entry TTL.** `SetTTL(key, v, ttl)`; `ttl <= 0` means the entry never expires. Expiry is lazy (checked on the `Get` that touches the key).
- **LRU bound.** `WithCapacity(n)` evicts the least-recently-used entry when full. Omit it (or pass a non-positive `n`) for an unbounded cache.
- **Eviction hook.** `WithOnEvict` fires when an entry is dropped *involuntarily* (capacity or expiry) — the place to close handles or release resources.
- **Stats.** `Stats()` returns the shared `cache.Stats` shape (hits/misses/sets/deletes/evictions + `EvictionsByCause` + entry gauge), so observability reads identically across the family.
- **Thread-safe.** All operations are safe for concurrent use, verified under `-race`.
- **A conformance suite.** `objtest.Run` *is* the contract; the built-in implementation passes it, and so must any alternative.

## API

```go
type Cache[T any] interface {
    Get(key string) (T, bool)
    Set(key string, v T)
    SetTTL(key string, v T, ttl time.Duration)
    Del(key string)
    Len() int
    Purge()
    Stats() cache.Stats
}

func New[T any](opts ...Option) *Store[T]
```

| Method | Purpose |
|---|---|
| `Get(key) (T, bool)` | Returns the live value (same reference) and `true`, or zero + `false` on miss/expiry. Expired entries are evicted as a side effect. |
| `Set(key, v)` | Insert or replace, using the default TTL (see `WithDefaultTTL`). |
| `SetTTL(key, v, ttl)` | Insert or replace with an explicit TTL. `ttl <= 0` ⇒ never expires. |
| `Del(key)` | Remove a key. No-op if absent. Does **not** fire `OnEvict`. |
| `Len()` | Current entry count (including expired-but-not-yet-swept). |
| `Purge()` | Drop every entry. Does **not** fire `OnEvict`. |
| `Stats()` | Point-in-time `cache.Stats` snapshot. |

## Options

```go
c := cacheobj.New[*http.Client](
    cacheobj.WithCapacity(1000),                 // LRU bound; omit for unbounded
    cacheobj.WithDefaultTTL(10*time.Minute),     // TTL applied by Set; SetTTL overrides
    cacheobj.WithOnEvict(func(key string, cause cache.EvictionCause) {
        // cause is cache.EvictSize (capacity) or cache.EvictExpired (TTL)
    }),
    cacheobj.WithClock(myFakeClock),             // deterministic TTL tests
)
```

`OnEvict` fires for **capacity** (`cache.EvictSize`) and **expiry** (`cache.EvictExpired`) only — the involuntary drops where you might need to release a live handle. Explicit `Del` / `Purge` do not fire it (you initiated those, so clean up at the call site). The callback runs while the cache lock is held — keep it fast and do not call back into the cache from it.

## Gotchas

> [!WARNING]
> **Returned objects are shared, not copied.** `Get` hands back the *same* reference every caller holds. That is the whole point (and impossible to avoid for non-copyable types), but it means a caller mutating a returned pointer mutates what everyone else sees. Treat cached objects as immutable, or synchronize mutation yourself.

- **Lazy expiry, no sweeper.** An expired entry is reclaimed on the next `Get` for its key, or when LRU capacity evicts it — there is no background janitor in this version. If you cache many short-TTL keys that are never read again, bound the cache with `WithCapacity` so they cannot accumulate.
- **In-process only.** Liveness cannot cross a process boundary; there is no network backend and never will be. That is `ubgo/cache`'s job.

## Relationship to the cache family

`cache-obj` is a sibling of `ubgo/cache`, not a backend of it. It imports the core only for the `Stats` and `EvictionCause` types so metrics look consistent. It is the family-branded successor to the deprecated `github.com/ubgo/threadsafecache`.

## License

Apache-2.0 — see [`LICENSE`](LICENSE).
