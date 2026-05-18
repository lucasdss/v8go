# W^X Enforcement via True Dual-Mapping

The existing CodeBuf uses a single `mmap` with `mprotect` toggling (RW ↔ RX) and CGO-dependent `pthread_jit_write_protect_np` on Darwin. Nine security and correctness issues were found during audit (see session plan). We decided to implement true dual-mapping: `memfd_create`/`shm_open` + two `mmap` calls mapping the same physical pages at two different virtual addresses — one RW for writes, one RX for execution. This eliminates CGO, provides simultaneous W and X access (no toggle window), and matches the architecture described in `docs/JIT-PROPOSAL.md`.

Considered alternatives: single-mapping with mprotect (current, has race window and CGO), single-mapping with Linux W^X enforcement but keeping CGO on Darwin (partial fix). Rejected because neither eliminates the toggle race window or the CGO dependency.
