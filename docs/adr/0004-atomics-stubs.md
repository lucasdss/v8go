# Atomics / SharedArrayBuffer as Compatibility Stubs

SharedArrayBuffer requires cross-thread shared memory between JavaScript agents (Web Workers). V8Go runs single-threaded per VM with no cross-VM shared memory architecture. Implementing true multi-threaded shared memory would require a material architectural change.

We decided to implement Atomics and SharedArrayBuffer as compatibility stubs. SharedArrayBuffer is identical to ArrayBuffer in behavior (single-threaded access is safe). All Atomics methods operate as regular non-atomic operations — single-threaded execution guarantees correctness without hardware atomic primitives. The full API surface is available for compatibility with libraries that check for `typeof SharedArrayBuffer !== 'undefined'`.

Considered alternatives: full cross-thread implementation via Go channels/sync primitives (rejected — requires Worker thread architecture, months of work), skipping entirely (rejected — leaves the last ECMAScript spec gap).
