## Repository Navigation (CRITICAL)
This project is indexed using `gograph`. **DO NOT use `grep` or `cat` for structural Go code analysis.**

1. Before answering architecture or repository questions, inspect the available `gograph_*` MCP tools for the current project and use them. Each project ships its own gograph MCP server; pick the matching one. If not started run `gograph mcp .` in the terminal and use the MCP.
2. If MCP tools are not available, run `gograph build .` in the terminal to ensure the index is fresh, then use the CLI commands (e.g., `gograph implementers <InterfaceName>`).
3. If the codebase is in a compilable state, building with `gograph build . --precise` enables strict type-checked interface analysis and highly precise call edges.
4. To extract a function body or mock stub without reading the whole file, use the source tool.
5. Use `grep` ONLY for string literals, configuration files (.env), or markdown documentation.

# RTK — Token-Optimized CLI

**rtk** is a CLI proxy that filters and compresses command outputs, saving 60-90% tokens.

## Rule

Always prefix shell commands with `rtk`:

```bash
# Instead of:              Use:
git status                 rtk git status
git log -10                rtk git log -10
cargo test                 rtk cargo test
docker ps                  rtk docker ps
kubectl get pods           rtk kubectl pods
```

## Meta commands (use directly)

```bash
rtk gain              # Token savings dashboard
rtk gain --history    # Per-command savings history
rtk discover          # Find missed rtk opportunities
rtk proxy <cmd>       # Run raw (no filtering) but track usage
```
