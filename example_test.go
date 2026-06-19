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

// WithOnEvict releases a live handle when the cache involuntarily drops it
// (capacity or expiry). Here capacity is 1, so the second Set evicts the
// first and the callback closes it.
func ExampleWithOnEvict() {
	c := cacheobj.New[string](
		cacheobj.WithCapacity(1),
		cacheobj.WithOnEvict(func(key string, cause cache.EvictionCause) {
			fmt.Printf("evicted %q (%s)\n", key, cause)
		}),
	)
	c.Set("a", "first")
	c.Set("b", "second") // evicts "a" by capacity
	// Output: evicted "a" (size)
}
