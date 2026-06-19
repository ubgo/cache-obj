// Command examples is a runnable tour of cache-obj: caching live objects
// (compiled regexes, an *http.Client), per-entry TTL, and the OnEvict hook.
//
//	go run ./examples
package main

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/ubgo/cache"
	cacheobj "github.com/ubgo/cache-obj"
)

func main() {
	liveRegexCache()
	httpClientPool()
	ttlAndStats()
	resourceCleanup()
}

// liveRegexCache memoizes compiled regular expressions — a value that cannot
// survive a codec, so it must be held by reference.
func liveRegexCache() {
	fmt.Println("== live regex cache ==")
	rc := cacheobj.New[*regexp.Regexp](cacheobj.WithCapacity(64))

	get := func(pattern string) *regexp.Regexp {
		if r, ok := rc.Get(pattern); ok {
			return r
		}
		r := regexp.MustCompile(pattern)
		rc.Set(pattern, r)
		return r
	}

	a := get(`^\d{3}-\d{4}$`)
	b := get(`^\d{3}-\d{4}$`) // served from cache — same instance
	fmt.Printf("same compiled instance: %v\n", a == b)
	fmt.Printf("matches 555-1234: %v\n\n", a.MatchString("555-1234"))
}

// httpClientPool caches configured *http.Client values by host. A decoded
// copy would lose the live transport — only a by-reference cache works.
func httpClientPool() {
	fmt.Println("== http client pool ==")
	clients := cacheobj.New[*http.Client](cacheobj.WithCapacity(16))

	clientFor := func(host string) *http.Client {
		if c, ok := clients.Get(host); ok {
			return c
		}
		c := &http.Client{Timeout: 5 * time.Second}
		clients.Set(host, c)
		return c
	}

	x := clientFor("api.example.com")
	y := clientFor("api.example.com")
	fmt.Printf("reused client: %v\n\n", x == y)
}

// ttlAndStats shows per-entry TTL with a controllable clock, the OnEvict hook,
// and the Stats snapshot.
func ttlAndStats() {
	fmt.Println("== ttl + onEvict + stats ==")
	now := time.Unix(1_000_000, 0)
	clock := func() time.Time { return now }

	c := cacheobj.New[string](
		cacheobj.WithCapacity(2),
		cacheobj.WithClock(clock),
		cacheobj.WithOnEvict(func(key, value string, cause cache.EvictionCause) {
			fmt.Printf("evicted %q=%q (%s)\n", key, value, cause)
		}),
	)

	c.SetTTL("session", "token-abc", 30*time.Second)
	c.Set("a", "1")
	c.Set("b", "2") // capacity 2 reached; "session" is LRU → evicted (size)

	if v, ok := c.Get("a"); ok {
		fmt.Printf("a = %s\n", v)
	}

	st := c.Stats()
	fmt.Printf("stats: hits=%d misses=%d sets=%d evictions=%d hitRatio=%.2f\n",
		st.Hits, st.Misses, st.Sets, st.Evictions, st.HitRatio())
}

// conn is a stand-in for a resource-owning value (e.g. *sql.DB) that must be
// closed when it leaves the cache.
type conn struct{ name string }

func (c *conn) Close() { fmt.Printf("closed conn %q\n", c.name) }

// resourceCleanup shows the value-bearing OnEvict closing handles owned by
// evicted values — the reason to cache live, resource-owning objects.
func resourceCleanup() {
	fmt.Println("\n== resource cleanup on eviction ==")
	pool := cacheobj.New[*conn](
		cacheobj.WithCapacity(1),
		cacheobj.WithOnEvict(func(_ string, v *conn, _ cache.EvictionCause) {
			v.Close() // release the evicted value's resource
		}),
	)
	pool.Set("primary", &conn{name: "primary"})
	pool.Set("replica", &conn{name: "replica"}) // evicts "primary" → closed
	pool.Purge()                                // explicit: does NOT fire OnEvict
	fmt.Println("done (Purge did not close replica — explicit removal)")
}
