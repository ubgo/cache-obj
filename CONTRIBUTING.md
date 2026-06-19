# Contributing to cache-obj

Thanks for helping improve `cache-obj`. It is a small, focused module; the bar is high and the surface is intentionally tiny.

## The gate

Every change must pass the full local gate before review:

```sh
task check
```

which is equivalent to:

```sh
gofmt -w . && go vet ./... && golangci-lint run ./... && go test -race -count=2 ./... && go test -coverprofile=cover.out .
```

Requirements: **0 lint issues, 0 test failures, and 100% coverage of the product package** (the root package). `revive` requires a doc comment on every exported identifier, starting with the identifier's name.

> The conformance suite `objtest.Run` reports below 100% because its failure-assertion branches (`t.Fatal*`) only execute when a test fails — that is expected and is not counted against the product package, which stays at 100%.

## The contract is the suite

`objtest.Run` **is** the contract. Any change to cache semantics changes `objtest` first, then the implementation — never the other way around. A new behavior without a corresponding `objtest` check is incomplete.

## Scope discipline

`cache-obj` does one thing: hold live objects by reference, in-process, with TTL + LRU bounds. Out of scope by design (these belong to `github.com/ubgo/cache`):

- serialization / codecs / encryption
- network or persistent backends
- namespacing, cross-process invalidation
- implementing the `cache.Cache` interface

If a feature request needs any of those, it belongs in the byte-cache family, not here.

## Dependencies

Keep the dependency set minimal: `hashicorp/golang-lru/v2` for storage and `github.com/ubgo/cache` for the shared `Stats` / `EvictionCause` types. New third-party dependencies need a strong justification.

## Commits

Conventional, imperative subject lines. Do not include tooling/agent attribution.
