# CLAUDE.md — Master Agent Instructions

This file is automatically loaded by all agents and sub-agents in this repository.

## CRITICAL: Read and follow these instruction files

Every agent and sub-agent MUST read and internalize these two files before doing any work:

1. **`.github/copilot-instructions.md`** — Repository navigation (gograph instead of grep/cat for Go code) and RTK CLI proxy usage.
2. **`golang-agent.md`** — The Go Forge Architect: mechanical sympathy, security-first design, Triple-Check Refinement Loop, and pre-merge security gate.

### How to comply

On startup, immediately read both files:
- `view .github/copilot-instructions.md`
- `view golang-agent.md`

Then follow them strictly throughout the session. Key rules:

- **Go code analysis:** Use gograph, NOT grep/cat.
- **Shell commands:** Always prefix with `rtk`.
- **Go quality:** Execute the Triple-Check Refinement Loop before delivering code.
- **Pre-merge:** Run `make check-security` before merging to main.

## Quality Gates (Mandatory)

All new code MUST pass these gates before merge:

| Gate | Command | Threshold |
|------|---------|-----------|
| **Coverage** | `make test-cover-gate` | **≥ 80%** on `pkg/jit/...` + `pkg/js/...` |
| **Lint** | `make lint-new` | 0 new issues vs `origin/main` |
| **Vulnerabilities** | `make vuln` | 0 findings |
| **Static analysis** | `make vet` | 0 issues |
| **Security scan** | `make sec-gate` | ≤ 12 gosec findings |
| **Full gate** | `make check-security` | All above + SAST + dependency scan |

**Code coverage minimum: 80%.** Tests must exercise all new functions, error paths, and edge cases. Coverage is measured on `pkg/jit/...` and `pkg/js/...`. Falling below 80% blocks merge.

**Lint minimum: 0 new issues.** Only issues introduced on the current branch vs `origin/main` are counted. Pre-existing baseline is exempt.

## Sub-agent dispatch protocol

When spawning sub-agents via the `task` tool, the prompt MUST include:

```
CRITICAL: Follow CLAUDE.md instructions. Use gograph for Go code analysis. Prefix shell commands with rtk. Apply Go Forge Architect standards from golang-agent.md.
```
