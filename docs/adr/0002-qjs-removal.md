# Remove QuickJS Comparison Engine

The QuickJS-based comparison engine (`pkg/js/engine.go`, `jsengine.go`, `eventloop.go`, `worker.go`, `scheduler.go`) was removed entirely. It imported `github.com/fastschema/qjs` (a CGO QuickJS binding) and transitively pulled in `github.com/tetratelabs/wazero`, bloating the module for `go.dev/play` and `go get` users.

V8Go is the sole JavaScript engine. The QuickJS engine was only used for browser comparison testing — no production feature depended on it. The `JSEngine` interface existed to swap between implementations, which is unnecessary with a single engine.

Considered alternatives: build tag isolation (`//go:build qjs`). Rejected because `go.mod` still listed the dependency, meaning `go get` would download it regardless. Removing entirely makes the module truly lightweight (2 dependencies instead of 5).
