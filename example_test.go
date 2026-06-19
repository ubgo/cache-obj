package cacheobj_test

import (
	"fmt"
	"regexp"
	"time"

	"github.com/ubgo/cache"
	cacheobj "github.com/ubgo/cache-obj"
)

// The headline use case: cache a live, non-serializable object — here a
// compiled *regexp.Regexp — and get the exact same instance back, with no
// recompilation and no serialization.
func Example() {
	re := cacheobj.New[*regexp.Regexp](cacheobj.WithCapacity(128))

	compile := func(pattern string) *regexp.Regexp {
		if r, ok := re.Get(pattern); ok {
			return r // same compiled program, zero cost
		}
		r := regexp.MustCompile(pattern)
		re.Set(pattern, r)
		return r
	}

	a := compile(`\d+`)
	b := compile(`\d+`) // served from cache — identical instance
	fmt.Println(a == b)
	fmt.Println(a.MatchString("abc123"))
	// Output:
	// true
	// true
}

// Remember is get-or-load with single-flight: the loader runs once on a miss
// (and once across concurrent misses for the same key), and the result is
// cached. A second call is served from cache without re-running the loader.
func ExampleStore_Remember() {
	c := cacheobj.New[int]()
	calls := 0
	load := func() (int, error) { calls++; return 42, nil }

	v1, _ := c.Remember("answer", time.Minute, load)
	v2, _ := c.Remember("answer", time.Minute, load) // cached — loader skipped
	fmt.Println(v1, v2, calls)
	// Output: 42 42 1
}

// SetTTL stores a value with an explicit lifetime; a non-positive TTL means
// the entry never expires.
func ExampleStore_SetTTL() {
	c := cacheobj.New[string]()
	c.SetTTL("token", "secret", 5*time.Minute)
	c.SetTTL("config", "immutable", 0) // 0 => never expires

	v, ok := c.Get("token")
	fmt.Println(v, ok)
	// Output: secret true
}

// WithOnEvict observes involuntary drops (capacity or expiry) and receives
// the evicted key AND value — so it can release whatever the value owns. Here
// capacity is 1, so the second Set evicts the first.
func ExampleWithOnEvict() {
	c := cacheobj.New[string](
		cacheobj.WithCapacity(1),
		cacheobj.WithOnEvict(func(key, value string, cause cache.EvictionCause) {
			fmt.Printf("evicted %q=%q (%s)\n", key, value, cause)
		}),
	)
	c.Set("a", "first")
	c.Set("b", "second") // evicts "a" by capacity
	// Output: evicted "a"="first" (size)
}
