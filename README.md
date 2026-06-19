# cache-obj


[![Go Reference](https://pkg.go.dev/badge/github.com/ubgo/cache-obj.svg)](https://pkg.go.dev/github.com/ubgo/cache-obj) [![Go Report Card](https://goreportcard.com/badge/github.com/ubgo/cache-obj)](https://goreportcard.com/report/github.com/ubgo/cache-obj) [![test](https://github.com/ubgo/cache-obj/actions/workflows/test.yml/badge.svg)](https://github.com/ubgo/cache-obj/actions/workflows/test.yml) [![lint](https://github.com/ubgo/cache-obj/actions/workflows/lint.yml/badge.svg)](https://github.com/ubgo/cache-obj/actions/workflows/lint.yml) ![coverage](https://img.shields.io/badge/coverage-100%25-brightgreen) [![tag](https://img.shields.io/github/v/tag/ubgo/cache-obj?sort=semver)](https://github.com/ubgo/cache-obj/tags) [![license](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE) ![Go](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)


**An in-process, zero-serialization cache for Go that holds live objects by reference.** What you `Set` is the *exact* instance you `Get` back — no codec, no copy, no `interface{}` boxing. Generic over the value type, thread-safe, with per-entry TTL, LRU bounds, a value-bearing eviction hook, single-flight `Remember`, and an optional background expiry sweeper.

`cache-obj` is the typed, in-process companion to [`github.com/ubgo/cache`](https://github.com/ubgo/cache). The byte cache serializes every value so it can work uniformly over memory/Redis/Postgres; `cache-obj` makes the opposite trade — it keeps values *alive in process* so you can cache things that cannot survive a codec round-trip: a compiled `*regexp.Regexp`, an `*http.Client`, an open connection, a rate limiter, an ORM entity you traverse.

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
> **`cache-obj` is NOT a `cache.Cache` backend.** It is a *different abstraction*: typed (`Cache[T]`), in-process only, no serialization. It deliberately does not implement `cache.Cache` and cannot be swapped into a Redis/Postgres slot. For **serializable** values (DTOs, configs, scalars, `[]Result`) use [`ubgo/cache`](https://github.com/ubgo/cache) + `cache-mem` — it is strictly more capable there (network backends, negative caching, refresh-ahead/SWR). Reach for `cache-obj` **only** when the value must stay live.

---

## Contents

- [When to use which](#when-to-use-which)
- [Use cases](#use-cases)
- [Install](#install)
- [Quick start](#quick-start)
- [Features](#features)
- [API](#api)
- [Options](#options)
- [Semantics & contract](#semantics--contract)
- [How `Remember` deduplicates loads](#how-remember-deduplicates-loads)
- [Concurrency & locking](#concurrency--locking)
- [Stats & observability](#stats--observability)
- [Performance](#performance)
- [Testing your code](#testing-your-code)
- [Conformance suite (`objtest`)](#conformance-suite-objtest)
- [Recipes](#recipes)
- [Gotchas](#gotchas)
- [FAQ](#faq)
- [Relationship to the cache family](#relationship-to-the-cache-family)
- [License](#license)

---

## When to use which

| You want to cache… | Use |
|---|---|
| A DTO, config, scalar, or any serializable value | `ubgo/cache` + `cache-mem` |
| The same value over Redis / Postgres / tiered | `ubgo/cache` + a network backend |
| A `*regexp.Regexp`, `*http.Client`, open connection, `func`, `chan` | **`cache-obj`** |
| An object with unexported state that must survive intact (e.g. an ORM entity you traverse/mutate) | **`cache-obj`** |

The dividing question: **after `Get`, do you need the *original object*, or just its *data*?** Original object (liveness) → `cache-obj`. Just the data → `ubgo/cache`.

Why a codec breaks live objects: serialization can only carry exported fields and value data. It cannot round-trip an unexported mutex, a live network transport, a compiled program, a function pointer, or an ORM client handle. A decoded copy *looks* fine for scalar reads but is dead for anything that needs the original internals — and it is a fresh allocation, so shared state is lost.

## Use cases

Each is a value that is expensive to build *and* cannot (or should not) be serialized:

- **Compiled regular expressions** (`*regexp.Regexp`) — compile once, reuse the program.
- **Parsed templates** (`*template.Template`, `*pongo2.Template`) — parse once, render many times.
- **HTTP clients per host/tenant** (`*http.Client`) — keep connection pools and configured transports alive.
- **gRPC / DB handles** (`*grpc.ClientConn`, `*sql.DB`, `*sql.Stmt`) — pool live handles keyed by target; close them on eviction via `OnEvict`.
- **Per-key rate limiters** (`*rate.Limiter`) — one live limiter per user/IP/route, carrying its token-bucket state.
- **Compiled validators / schemas** (JSON-schema, CEL programs, query plans) — build the evaluator once.
- **Live ORM entities for traversal** (an ent `*ent.User`) — keep the client binding so `.QueryEdges()` / `.Update()` work; a decoded copy would null it.
- **Loaded models / parsers / interpreters** — anything with a heavy constructor and internal state that must stay resident.

If the value is a plain DTO, config struct, scalar, or anything you only read *fields* off, you do **not** need `cache-obj` — use `ubgo/cache` + `cache-mem`.

## Install

```sh
go get github.com/ubgo/cache-obj
```

Requires Go 1.24+. Dependencies: `hashicorp/golang-lru/v2` (storage) and `github.com/ubgo/cache` (imported solely for the shared `Stats` / `EvictionCause` types).

## Quick start

A complete, runnable program:

```go
package main

import (
    "fmt"
    "regexp"
    "time"

    cacheobj "github.com/ubgo/cache-obj"
)

func main() {
    // A bounded cache of compiled regexes with a 1-hour TTL.
    re := cacheobj.New[*regexp.Regexp](
        cacheobj.WithCapacity(1024),
        cacheobj.WithDefaultTTL(time.Hour),
    )

    // Get-or-load with single-flight: compiles once, even under concurrency.
    digits, _ := re.Remember(`\d+`, time.Hour, func() (*regexp.Regexp, error) {
        return regexp.Compile(`\d+`)
    })
    fmt.Println(digits.MatchString("abc123")) // true

    // A second Get returns the exact same instance.
    again, _ := re.Get(`\d+`)
    fmt.Println(again == digits) // true

    fmt.Printf("%+v\n", re.Stats()) // {Hits:1 Misses:1 Sets:1 ...}
}
```

See [`examples/main.go`](examples/main.go) for a fuller tour (regex cache, HTTP client pool, TTL + stats, resource cleanup on eviction), runnable with `go run ./examples`.

## Features

- **Live objects, by reference.** `Get` returns the same instance you `Set` — no serialization, no copy. The only cache in the family that can hold non-serializable values.
- **Generics.** `Cache[T]` stores `T` directly, no `interface{}` boxing.
- **Per-entry TTL.** `SetTTL(key, v, ttl)`; `ttl <= 0` means the entry never expires. `WithDefaultTTL` sets the TTL `Set` applies.
- **LRU bound.** `WithCapacity(n)` evicts the least-recently-used entry when full. Omit it for an unbounded cache.
- **Value-bearing eviction hook.** `WithOnEvict(func(key string, v T, cause cache.EvictionCause))` fires when an entry is dropped *involuntarily* (capacity or expiry) and hands you the value — so you can close handles or release resources.
- **Single-flight `Remember`.** Get-or-load that collapses a thundering herd: N concurrent misses for the same key run the loader once and share the result.
- **Optional background sweeper.** `WithSweepInterval(d)` proactively evicts expired entries; `Close()` stops it. Default expiry is lazy (no goroutine).
- **Stats.** `Stats()` returns the shared `cache.Stats` shape, so observability reads identically across the family.
- **Thread-safe.** Every operation is safe for concurrent use, verified under `-race`.
- **A conformance suite.** `objtest.Run` *is* the contract; the built-in implementation passes it, and so must any alternative.
- **Small & focused.** One product file, one storage dependency, 100% test coverage.

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

// On the concrete *Store[T] returned by New (kept off the minimal interface):
func (s *Store[T]) Remember(key string, ttl time.Duration, fn func() (T, error)) (T, error)
func (s *Store[T]) Close() // stops the background sweeper; idempotent
```

| Method | Purpose |
|---|---|
| `Get(key) (T, bool)` | Returns the live value (same reference) and `true`, or zero + `false` on miss/expiry. An expired entry is evicted as a side effect. |
| `Set(key, v)` | Insert or replace, using the default TTL (see `WithDefaultTTL`). |
| `SetTTL(key, v, ttl)` | Insert or replace with an explicit TTL. `ttl <= 0` ⇒ never expires. |
| `Del(key)` | Remove a key. No-op if absent. Does **not** fire `OnEvict`. |
| `Len()` | Current entry count (including expired-but-not-yet-swept). |
| `Purge()` | Drop every entry. Does **not** fire `OnEvict`. |
| `Stats()` | Point-in-time `cache.Stats` snapshot. |
| `Remember(key, ttl, fn)` | Get-or-load with single-flight: loads via `fn` once under concurrent misses, stores with `ttl`. Errors are returned, not cached. |
| `Close()` | Stops the background sweeper (if any). Idempotent; no-op without `WithSweepInterval`. |

## Options

```go
c := cacheobj.New[*http.Client](
    cacheobj.WithCapacity(1000),                 // LRU bound; omit for unbounded
    cacheobj.WithDefaultTTL(10*time.Minute),     // TTL applied by Set; SetTTL overrides
    cacheobj.WithOnEvict(func(key string, v *http.Client, cause cache.EvictionCause) {
        // cause is cache.EvictSize (capacity) or cache.EvictExpired (TTL); v is the evicted value
    }),
    cacheobj.WithSweepInterval(time.Minute),     // background expiry sweeper (else lazy); call Close to stop
    cacheobj.WithClock(myFakeClock),             // deterministic TTL tests
)
```

| Option | Effect | Default |
|---|---|---|
| `WithCapacity(n)` | LRU-bound to `n` entries; non-positive ⇒ unbounded | unbounded |
| `WithDefaultTTL(d)` | TTL applied by `Set`; non-positive ⇒ no expiry | no expiry |
| `WithOnEvict(fn)` | Callback on involuntary eviction (capacity/expiry), with key + value | none |
| `WithSweepInterval(d)` | Background goroutine evicting expired entries every `d`; non-positive ⇒ lazy | lazy (no goroutine) |
| `WithClock(now)` | Override the time source (deterministic tests) | `time.Now` |

`OnEvict` fires for **capacity** (`cache.EvictSize`) and **expiry** (`cache.EvictExpired`) only — the involuntary drops where you may need to release the evicted value's resources. The value's type is **inferred** from the callback (no type parameter) and must match the cache's `T`. Explicit `Del` / `Purge` do **not** fire it (you initiated those — clean up at the call site). The callback runs while the cache lock is held — keep it fast and do not call back into the cache from it.

## Semantics & contract

These invariants are enforced by [`objtest.Run`](#conformance-suite-objtest):

- **Miss is `(zero, false)`.** `Get` on an absent or expired key returns the zero value and `false` — never a stale value with `false`, never a real value lost behind a `true` mismatch.
- **Same reference.** For pointer/interface `T`, `Get` returns the *identical* instance passed to `Set` (`got == want`). No defensive copy.
- **`ttl <= 0` ⇒ immortal.** Such an entry never expires; it lives until evicted by capacity or removed by `Del`/`Purge`.
- **Lazy expiry.** Without a sweeper, an entry past its TTL is detected and evicted on the `Get` that touches it. `Len` may include expired-but-untouched entries until they are read or swept.
- **`OnEvict` only for involuntary drops.** Capacity (`EvictSize`) and expiry (`EvictExpired`) fire it; `Del`, `Purge`, and overwriting an existing key do not.
- **Errors are not cached.** `Remember` returns a loader error to every waiting caller and stores nothing; the next call retries.
- **`Close` is idempotent** and safe to call when no sweeper was started.

## How `Remember` deduplicates loads

`Remember` adds **single-flight**: when many goroutines miss the same cold key at once, the loader runs **exactly once** and the rest wait and share that one result — no thundering herd on your database or RPC backend.

```
50 goroutines call Remember("user:42") at once
        │
        ▼
   Get("user:42") → MISS for all 50
        │
        ▼  each grabs an internal lock briefly:
   ┌─────────────────────────────────────────────┐
   │ goroutine #1 (LEADER):                       │
   │   key not in flight map → register call{}    │
   │   release lock, run loader()  ← the ONE load │
   ├─────────────────────────────────────────────┤
   │ goroutines #2..#50 (FOLLOWERS):              │
   │   key IS in flight map → grab the call{}      │
   │   release lock, wait on its WaitGroup ← BLOCK │
   └─────────────────────────────────────────────┘
        │
   leader finishes → stores result in call{}, wg.Done()
        │
        ▼
   all 49 followers wake, read the shared result
   → 50 callers, 1 load; then the flight entry is removed
```

The first goroutine to grab the lock becomes the leader and registers an in-flight `call` in a map *before* releasing the lock; every later goroutine finds it already there and waits on its `WaitGroup` instead of loading. Different keys never block each other (the flight is per-key).

> The loader **must not** call `Remember` for the same key (it would wait on itself — deadlock) and should not panic (a panic propagates to the leader; waiters are released but observe the zero value).

## Concurrency & locking

- Every operation is guarded by a single `sync.Mutex`. (Not an `RWMutex`: `hashicorp/golang-lru` mutates recency state on `Get`, so reads are writes underneath.)
- Counters live under the same lock — no atomics, no torn reads.
- `OnEvict` runs **while the lock is held**. Keep it fast; never call back into the cache from it (re-entrant lock → deadlock). For slow cleanup, hand the value to a background worker.
- The sweeper goroutine takes the same lock for each pass; pick an interval suited to the cache size.
- Verified race-clean under `go test -race -count=2`.

## Stats & observability

`Stats()` returns `github.com/ubgo/cache.Stats` — the same shape the whole family reports, so one dashboard works across backends:

| Field | Meaning |
|---|---|
| `Hits` / `Misses` | cumulative `Get` outcomes |
| `Sets` / `Deletes` | cumulative `Set`/`SetTTL` and `Del` calls |
| `Evictions` | total involuntary drops |
| `EvictionsByCause` | breakdown keyed by `cache.EvictSize` / `cache.EvictExpired` |
| `Entries` | instantaneous entry count |
| `HitRatio()` | `Hits / (Hits+Misses)`, `0` when no traffic |

```go
s := c.Stats()
log.Printf("entries=%d hitRatio=%.2f evictions=%d (size=%d expired=%d)",
    s.Entries, s.HitRatio(), s.Evictions,
    s.EvictionsByCause[cache.EvictSize], s.EvictionsByCause[cache.EvictExpired])
```

## Performance

- `Get` / `Set` / `SetTTL` / `Del` are **O(1)**.
- `Get` does **zero serialization and zero allocation** on a hit — it returns the stored reference directly. This is the core advantage over a byte cache, which decodes (and allocates) on every `Get`.
- `Stats()` allocates one small map (a copy of `EvictionsByCause`) so the snapshot is safe to mutate.
- `sweep()` is **O(n)** in entry count and holds the lock for the pass — size the sweep interval accordingly, or rely on lazy expiry + `WithCapacity`.

## Testing your code

`WithClock` injects a deterministic time source so TTL behavior is testable without sleeps:

```go
now := time.Unix(1_000_000, 0)
clock := func() time.Time { return now }

c := cacheobj.New[string](cacheobj.WithClock(clock))
c.SetTTL("k", "v", time.Minute)

now = now.Add(2 * time.Minute) // advance virtual time
if _, ok := c.Get("k"); ok {
    t.Fatal("entry should have expired")
}
```

## Conformance suite (`objtest`)

`objtest.Run` is the executable contract. The built-in `Store` passes it; if you write an alternative `Cache[T]`, run it against the same suite:

```go
import (
    cacheobj "github.com/ubgo/cache-obj"
    "github.com/ubgo/cache-obj/objtest"
)

func TestMyCache(t *testing.T) {
    objtest.Run(t, true /* bounded */, func(opts ...cacheobj.Option) cacheobj.Cache[*objtest.Val] {
        return cacheobj.New[*objtest.Val](append([]cacheobj.Option{cacheobj.WithCapacity(2)}, opts...)...)
    })
}
```

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
    return cache.Remember(pattern, 0, func() (*regexp.Regexp, error) {
        return regexp.Compile(pattern)
    })
}
```

### Single-flight loading with `Remember`

`Remember` is get-or-load with **single-flight**: under concurrent cold misses the loader runs **once** and the rest share the result. Loader errors are returned to all callers and not cached (the next call retries).

```go
users := cacheobj.New[*ent.User](cacheobj.WithCapacity(10_000))

u, err := users.Remember(id, 15*time.Minute, func() (*ent.User, error) {
    return client.User.Get(ctx, id) // runs once even under N concurrent misses
})
```

### Caching a live ORM entity you will traverse

The case `ubgo/cache` cannot serve: you need the *live* entity (its client binding intact) so downstream code can traverse edges or mutate it. A codec round-trip would null the binding.

```go
u, err := users.Remember(id, 15*time.Minute, func() (*ent.User, error) {
    return client.User.Get(ctx, id) // live entity, ent client still attached
})
// u.QueryPosts().All(ctx) works — it would panic on a decoded copy
```

> Reminder: the cached `*ent.User` is shared. If you mutate it in place, every holder sees the change. Cache a flat DTO instead if you only need its fields.

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

### Releasing handles on eviction

`OnEvict` receives the evicted **key and value**, so it can release whatever the value owns (close a `*sql.DB`, drain a pool). It fires only on capacity/expiry — not on `Del`/`Purge`.

```go
pool := cacheobj.New[*sql.DB](
    cacheobj.WithCapacity(32),
    cacheobj.WithDefaultTTL(time.Hour),
    cacheobj.WithOnEvict(func(key string, db *sql.DB, cause cache.EvictionCause) {
        _ = db.Close() // the evicted value, closed as it leaves the cache
    }),
)
```

### Background expiry sweeper

By default expiry is lazy. For a cache of short-TTL keys that may never be read again, a sweeper proactively reclaims them. It runs a goroutine — call `Close` when done.

```go
sessions := cacheobj.New[*Session](
    cacheobj.WithDefaultTTL(30*time.Minute),
    cacheobj.WithSweepInterval(time.Minute), // evict expired entries every minute
    cacheobj.WithOnEvict(func(id string, s *Session, _ cache.EvictionCause) {
        s.flush() // sweeper fires OnEvict(EvictExpired) for each reclaimed entry
    }),
)
defer sessions.Close() // stops the sweeper goroutine; idempotent
```

### Periodic stats logging

```go
go func() {
    for range time.Tick(time.Minute) {
        s := cache.Stats()
        log.Printf("cache: entries=%d hits=%d misses=%d hitRatio=%.2f evictions=%d",
            s.Entries, s.Hits, s.Misses, s.HitRatio(), s.Evictions)
    }
}()
```

### Unbounded vs bounded

```go
// Bounded: at most N entries, LRU eviction when full.
bounded := cacheobj.New[string](cacheobj.WithCapacity(500))

// Unbounded: grows until entries are deleted or expire. Pair with a TTL
// (and/or a sweeper) so it cannot grow without limit.
unbounded := cacheobj.New[string](cacheobj.WithDefaultTTL(5 * time.Minute))
```

## Gotchas

> [!WARNING]
> **Returned objects are shared, not copied.** `Get` hands back the *same* reference every caller holds. That is the whole point (and impossible to avoid for non-copyable types), but it means a caller mutating a returned pointer mutates what everyone else sees. Treat cached objects as immutable, or synchronize mutation yourself.

- **Lazy expiry by default.** An expired entry is reclaimed on the next `Get` for its key, or when LRU capacity evicts it. If you cache many short-TTL keys that are never read again, bound the cache with `WithCapacity` or enable `WithSweepInterval` (and call `Close`).
- **`OnEvict` runs under the lock.** Keep it fast; never call back into the cache from it. Hand slow cleanup to a background worker.
- **The sweeper goroutine must be stopped.** If you use `WithSweepInterval`, call `Close` when you discard the cache, or the goroutine (and the cache it references) leaks.
- **In-process only.** Liveness cannot cross a process boundary; there is no network backend and never will be. That is `ubgo/cache`'s job.

## FAQ

**Why not just use `sync.Map`?** You can, for the simplest cases. `cache-obj` adds TTL, LRU bounds, eviction hooks, single-flight loading, and stats — the things you end up re-implementing around a `sync.Map` once the cache matters.

**Why doesn't it implement `cache.Cache`?** Because that interface is `[]byte`-in/`[]byte`-out, and any value crossing it loses liveness through the codec. A live-object path only works in-process, so it would break the family's "one contract, every backend" guarantee. Different abstraction, different (smaller) interface.

**Does `Remember` cache errors?** No. A loader error is returned to all waiting callers and nothing is stored; the next call retries. (The byte cache offers negative caching via an envelope; `cache-obj` has no envelope.)

**Can the `OnEvict` callback receive the evicted value?** Yes — that is the default. The callback is `func(key string, v T, cause cache.EvictionCause)`; `T` is inferred from your closure.

**Is `Get` allocation-free?** On a hit, yes — it returns the stored reference with no decode and no allocation.

## Relationship to the cache family

`cache-obj` is a sibling of [`ubgo/cache`](https://github.com/ubgo/cache), not a backend of it. It imports the core only for the `Stats` and `EvictionCause` types so metrics look consistent across the family. It is the family-branded successor to the deprecated [`github.com/ubgo/threadsafecache`](https://github.com/ubgo/threadsafecache).

## License

Apache-2.0 — see [`LICENSE`](LICENSE).
