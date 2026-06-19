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

## Use cases

Concrete things people cache with `cache-obj` — each is a value that is expensive to build *and* cannot (or should not) be serialized:

- **Compiled regular expressions** (`*regexp.Regexp`) — compile once, reuse the program.
- **Parsed templates** (`*template.Template`, `*pongo2.Template`) — parse once, render many times.
- **HTTP clients per host/tenant** (`*http.Client`) — keep connection pools and configured transports alive.
- **gRPC / DB connections** (`*grpc.ClientConn`, `*sql.DB`, `*sql.Stmt`) — pool live handles keyed by target; close them on eviction via `OnEvict`.
- **Per-key rate limiters** (`*rate.Limiter`) — one live limiter per user/IP/route, carrying its token-bucket state.
- **Compiled validators / schemas** (JSON-schema, CEL programs, query plans) — build the evaluator once.
- **Live ORM entities for traversal** (an ent `*ent.User`) — keep the client binding so `.QueryEdges()` / `.Update()` work; a decoded copy would null it.
- **Loaded models / parsers / interpreters** — anything with a heavy constructor and internal state that must stay resident.

If the value is a plain DTO, config struct, scalar, or anything you only read *fields* off, you do **not** need `cache-obj` — use `ubgo/cache` + `cache-mem`.

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
- **Single-flight `Remember`.** Get-or-load that collapses a thundering herd: N concurrent misses for the same key run the loader once and share the result.
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

// On the concrete *Store[T] (not the interface):
func (s *Store[T]) Remember(key string, ttl time.Duration, fn func() (T, error)) (T, error)
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
| `Remember(key, ttl, fn)` | Get-or-load with single-flight: loads via `fn` once under concurrent misses, stores with `ttl`. Errors are returned, not cached. (`*Store[T]` method, not on the interface.) |

## Options

```go
c := cacheobj.New[*http.Client](
    cacheobj.WithCapacity(1000),                 // LRU bound; omit for unbounded
    cacheobj.WithDefaultTTL(10*time.Minute),     // TTL applied by Set; SetTTL overrides
    cacheobj.WithOnEvict(func(key string, v *http.Client, cause cache.EvictionCause) {
        // cause is cache.EvictSize (capacity) or cache.EvictExpired (TTL); v is the evicted value
    }),
    cacheobj.WithClock(myFakeClock),             // deterministic TTL tests
)
```

`OnEvict` fires for **capacity** (`cache.EvictSize`) and **expiry** (`cache.EvictExpired`) only — the involuntary drops where you may need to release the evicted value's resources. It receives the key and value (the value's type is inferred and must match the cache's `T`). Explicit `Del` / `Purge` do not fire it (you initiated those, so clean up at the call site). The callback runs while the cache lock is held — keep it fast and do not call back into the cache from it.

## Gotchas

> [!WARNING]
> **Returned objects are shared, not copied.** `Get` hands back the *same* reference every caller holds. That is the whole point (and impossible to avoid for non-copyable types), but it means a caller mutating a returned pointer mutates what everyone else sees. Treat cached objects as immutable, or synchronize mutation yourself.

- **Lazy expiry, no sweeper.** An expired entry is reclaimed on the next `Get` for its key, or when LRU capacity evicts it — there is no background janitor in this version. If you cache many short-TTL keys that are never read again, bound the cache with `WithCapacity` so they cannot accumulate.
- **In-process only.** Liveness cannot cross a process boundary; there is no network backend and never will be. That is `ubgo/cache`'s job.

## Recipes

### Package-level singleton

The common shape: one process-wide cache, initialized once.

```go
package regexcache

import (
    "regexp"

    cacheobj "github.com/ubgo/cache-obj"
)

var cache = cacheobj.New[*regexp.Regexp](cacheobj.WithCapacity(1024))

// Get returns a compiled regex, compiling and caching on first use.
func Get(pattern string) (*regexp.Regexp, error) {
    if r, ok := cache.Get(pattern); ok {
        return r, nil
    }
    r, err := regexp.Compile(pattern)
    if err != nil {
        return nil, err
    }
    cache.Set(pattern, r)
    return r, nil
}
```

### Single-flight loading with `Remember`

`Remember` is get-or-load with **single-flight**: when many goroutines miss the same cold key at once, the loader runs **exactly once** and the others wait and share the result — no thundering herd on your DB/RPC. Loader errors are returned to all callers and not cached (the next call retries).

```go
users := cacheobj.New[*ent.User](cacheobj.WithCapacity(10_000))

u, err := users.Remember(id, 15*time.Minute, func() (*ent.User, error) {
    return client.User.Get(ctx, id) // runs once even under N concurrent misses
})
```

`Remember` is a method on the concrete `*Store[T]` returned by `New` (not on the `Cache[T]` interface, which stays minimal). The loader must not call `Remember` for the same key (it would deadlock waiting on itself).

### Caching a live ORM entity you will traverse

The case `ubgo/cache` cannot serve: you need the *live* entity (its client binding intact) so downstream code can traverse edges or mutate it. A codec round-trip would null the binding.

```go
users := cacheobj.New[*ent.User](cacheobj.WithCapacity(10_000))

u, err := users.Remember(id, 15*time.Minute, func() (*ent.User, error) {
    return client.User.Get(ctx, id) // live entity, ent client still attached
})
// u.QueryPosts().All(ctx) works — it would panic on a decoded copy
```

> Reminder: the cached `*ent.User` is shared. If you mutate it in place, every holder sees the change. Cache a flat DTO instead if you only need its fields.

### Periodic stats logging

```go
go func() {
    for range time.Tick(time.Minute) {
        s := cache.Stats()
        log.Printf("cache: entries=%d hits=%d misses=%d hitRatio=%.2f evictions=%d (size=%d expired=%d)",
            s.Entries, s.Hits, s.Misses, s.HitRatio(), s.Evictions,
            s.EvictionsByCause[cache.EvictSize], s.EvictionsByCause[cache.EvictExpired])
    }
}()
```

### Releasing handles on eviction

`OnEvict` fires when an entry is dropped involuntarily — by capacity (`cache.EvictSize`) or TTL expiry (`cache.EvictExpired`) — and receives the evicted **key and value**, so it can release whatever the value owns (close a `*sql.DB`, drain a pool, etc.). It is **not** called for `Del` / `Purge` (those are deliberate — clean up at the call site).

```go
pool := cacheobj.New[*sql.DB](
    cacheobj.WithCapacity(32),
    cacheobj.WithDefaultTTL(time.Hour),
    cacheobj.WithOnEvict(func(key string, db *sql.DB, cause cache.EvictionCause) {
        _ = db.Close() // the evicted value, closed as it leaves the cache
    }),
)
```

The value's type is inferred from the callback — no type parameter needed — and must match the cache's `T`.

> [!NOTE]
> `OnEvict` runs while the cache lock is held: keep it fast and never call back into the cache from inside it. For slow cleanup, hand the value to a background closer rather than blocking the callback.

### Per-key rate limiter

One live `*rate.Limiter` per user/IP, each carrying its own token-bucket state — the limiter must be the *same instance* across requests, so a serializing cache would reset every caller's budget.

```go
limiters := cacheobj.New[*rate.Limiter](cacheobj.WithCapacity(100_000))

func allow(userID string) bool {
    lim, _ := limiters.Remember(userID, 0, func() (*rate.Limiter, error) {
        return rate.NewLimiter(rate.Every(time.Second), 10), nil // 10 rps, burst 10
    })
    return lim.Allow()
}
```

### Compiled template cache

```go
tpls := cacheobj.New[*template.Template](cacheobj.WithCapacity(256))

func render(w io.Writer, name, src string, data any) error {
    t, err := tpls.Remember(name, 0, func() (*template.Template, error) {
        return template.New(name).Parse(src) // parsed once, even under concurrency
    })
    if err != nil {
        return err
    }
    return t.Execute(w, data)
}
```

### Unbounded vs bounded

```go
// Bounded: at most N entries, LRU eviction when full.
bounded := cacheobj.New[string](cacheobj.WithCapacity(500))

// Unbounded: grows until entries are deleted or expire. Pair with a TTL
// so it cannot grow without limit.
unbounded := cacheobj.New[string](cacheobj.WithDefaultTTL(5 * time.Minute))
```

## Relationship to the cache family

`cache-obj` is a sibling of `ubgo/cache`, not a backend of it. It imports the core only for the `Stats` and `EvictionCause` types so metrics look consistent. It is the family-branded successor to the deprecated `github.com/ubgo/threadsafecache`.

## License

Apache-2.0 — see [`LICENSE`](LICENSE).
