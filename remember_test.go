package cacheobj_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cacheobj "github.com/ubgo/cache-obj"
)

func TestRememberHitSkipsLoader(t *testing.T) {
	c := cacheobj.New[int]()
	c.Set("k", 1)
	called := false
	v, err := c.Remember("k", 0, func() (int, error) {
		called = true
		return 99, nil
	})
	if err != nil || v != 1 {
		t.Fatalf("Remember hit = %d, %v; want 1, nil", v, err)
	}
	if called {
		t.Fatal("loader ran on a cache hit")
	}
}

func TestRememberLoadsAndStores(t *testing.T) {
	clk := newClock()
	c := cacheobj.New[int](cacheobj.WithClock(clk.now))

	v, err := c.Remember("k", 10*time.Second, func() (int, error) { return 7, nil })
	if err != nil || v != 7 {
		t.Fatalf("Remember miss = %d, %v; want 7, nil", v, err)
	}
	// Stored, so a second call hits without the loader.
	v2, _ := c.Remember("k", 10*time.Second, func() (int, error) { return 0, errors.New("must not run") })
	if v2 != 7 {
		t.Fatalf("second Remember = %d; want 7 (cached)", v2)
	}
	// Honors the TTL it was stored with.
	clk.advance(11 * time.Second)
	if _, ok := c.Get("k"); ok {
		t.Fatal("Remember-stored value did not expire per its ttl")
	}
}

func TestRememberErrorNotCached(t *testing.T) {
	c := cacheobj.New[int]()
	boom := errors.New("boom")
	if _, err := c.Remember("k", 0, func() (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("errored load should not have been cached")
	}
	// A subsequent successful load works (the error was not sticky).
	v, err := c.Remember("k", 0, func() (int, error) { return 5, nil })
	if err != nil || v != 5 {
		t.Fatalf("retry after error = %d, %v; want 5, nil", v, err)
	}
}

// TestRememberSingleFlight proves the headline behavior: N concurrent misses
// for the same key run the loader exactly once.
func TestRememberSingleFlight(t *testing.T) {
	c := cacheobj.New[*int]()

	const n = 50
	var loads int32
	started := make(chan struct{})
	release := make(chan struct{})

	results := make([]*int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			v, _ := c.Remember("k", 0, func() (*int, error) {
				if atomic.AddInt32(&loads, 1) == 1 {
					close(started) // leader is now inside the loader
					<-release      // hold the flight open while followers queue
				}
				val := 1
				return &val, nil
			})
			results[i] = v
		}(i)
	}

	<-started
	time.Sleep(50 * time.Millisecond) // let the other n-1 callers reach the flight
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&loads); got != 1 {
		t.Fatalf("loader ran %d times, want exactly 1", got)
	}
	for i, v := range results {
		if v == nil || v != results[0] {
			t.Fatalf("result[%d] = %p, want shared %p", i, v, results[0])
		}
	}
}

// TestRememberDistinctKeysParallel confirms different keys don't block each
// other (each runs its own loader).
func TestRememberDistinctKeysParallel(t *testing.T) {
	c := cacheobj.New[string]()
	var wg sync.WaitGroup
	for _, k := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			v, err := c.Remember(k, 0, func() (string, error) { return "v-" + k, nil })
			if err != nil || v != "v-"+k {
				t.Errorf("key %s = %q, %v", k, v, err)
			}
		}(k)
	}
	wg.Wait()
	if c.Len() != 3 {
		t.Fatalf("Len = %d, want 3", c.Len())
	}
}
