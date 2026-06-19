// cache.go — the live-object cache contract (package cacheobj,
// github.com/ubgo/cache-obj).
//
// This file declares the Cache[T] interface that every cacheobj
// implementation satisfies, plus the entry envelope used internally. The
// contract is enforced by objtest.Run; change semantics here and the suite
// changes in lockstep.

package cacheobj

import (
	"time"

	"github.com/ubgo/cache"
)

// Cache is the live-object cache contract: values are stored and returned by
// reference, with no serialization. It is generic over the value type T and
// is NOT github.com/ubgo/cache.Cache (see the package doc).
//
// All methods are safe for concurrent use.
type Cache[T any] interface {
	// Get returns the live value and true, or the zero value and false on a
	// miss or expiry. The returned value is the SAME reference that was Set
	// (no copy). A TTL'd entry found expired is evicted as a side effect.
	Get(key string) (T, bool)

	// Set stores v under key using the store's default TTL (see
	// WithDefaultTTL). An existing entry is replaced.
	Set(key string, v T)

	// SetTTL stores v under key with an explicit TTL. A ttl <= 0 means the
	// entry never expires (it lives until evicted by capacity or deleted).
	SetTTL(key string, v T, ttl time.Duration)

	// Del removes key. Deleting an absent key is a no-op. Del does not fire
	// the OnEvict hook — eviction notifications are for involuntary drops
	// (capacity, expiry), not explicit removal.
	Del(key string)

	// Len reports the current entry count, including any expired entries not
	// yet swept by a Get.
	Len() int

	// Purge removes every entry. Like Del, it does not fire OnEvict.
	Purge()

	// Stats returns a point-in-time snapshot using the shared
	// github.com/ubgo/cache.Stats shape. Counters are cumulative since
	// construction; Entries is an instantaneous gauge.
	Stats() cache.Stats
}

// entry is the internal envelope: the live value plus its absolute expiry
// instant. A zero expiresAt means "never expires".
type entry[T any] struct {
	value     T
	expiresAt time.Time
}
