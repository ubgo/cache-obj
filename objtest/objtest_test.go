package objtest_test

import (
	"testing"

	cacheobj "github.com/ubgo/cache-obj"
	"github.com/ubgo/cache-obj/objtest"
)

// These tests exercise the conformance suite itself (so the objtest package
// is covered) by running it against the reference cacheobj.New implementation.

func TestSuiteBounded(t *testing.T) {
	objtest.Run(t, true, func(opts ...cacheobj.Option) cacheobj.Cache[*objtest.Val] {
		return cacheobj.New[*objtest.Val](append([]cacheobj.Option{cacheobj.WithCapacity(2)}, opts...)...)
	})
}

func TestSuiteUnbounded(t *testing.T) {
	objtest.Run(t, false, func(opts ...cacheobj.Option) cacheobj.Cache[*objtest.Val] {
		return cacheobj.New[*objtest.Val](opts...)
	})
}
