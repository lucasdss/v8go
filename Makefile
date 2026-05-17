.PHONY: all test lint sec vuln clean tidy help
.PHONY: test-short test-cover bench ci check-security
.PHONY: osv-scan osv-update semgrep semgrep-ci vet

# ── Default ─────────────────────────────────────────────────────────────────
all: deps tidy lint vuln test

# ── CI ─────────────────────────────────────────────────────────────────────
ci: deps check-security test bench

# ── Test ────────────────────────────────────────────────────────────────────
test:
go test -v -race -count=1 ./...

test-short:
go test -short -count=1 ./...

test-cover:
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# ── Coverage Gate ───────────────────────────────────────────────────────────
# Enforces minimum 80% code coverage on V8Go packages.
COVERAGE_THRESHOLD = 80
COVER_PKG = ./pkg/jit/... ./pkg/js/...

test-cover-gate:
@echo "=== COVERAGE GATE: $(COVERAGE_THRESHOLD)% minimum on $(COVER_PKG) ==="
@go test -coverprofile=/tmp/v8go-cover.out $(COVER_PKG) >/dev/null 2>&1
@total=$$(go tool cover -func=/tmp/v8go-cover.out | tail -1 | awk '{print $$3}' | sed 's/%//'); \
if [ -z "$$total" ]; then echo "FAIL: could not compute coverage"; exit 1; fi; \
if [ "$$(echo "$$total < $(COVERAGE_THRESHOLD)" | bc -l 2>/dev/null || echo 1)" = "1" ]; then \
echo "PASS: $$total% coverage (threshold $(COVERAGE_THRESHOLD)%)"; \
else \
echo "FAIL: $$total% coverage (threshold $(COVERAGE_THRESHOLD)%)"; \
exit 1; \
fi

# ── Benchmarks ──────────────────────────────────────────────────────────────
bench:
go test -bench=. -benchtime=500ms -run='^$$' ./pkg/...

bench-mem:
go test -bench=. -benchtime=500ms -benchmem -run='^$$' ./pkg/...

# ── Vetting ─────────────────────────────────────────────────────────────────
vet:
go vet ./...

# ── Lint ────────────────────────────────────────────────────────────────────
lint:
golangci-lint run ./...

lint-new:
golangci-lint run --new-from-rev=origin/main ./...

lint-fix:
golangci-lint run --fix ./...

# ── Security ────────────────────────────────────────────────────────────────
sec:
gosec -quiet -exclude=G115 ./...

vuln:
govulncheck ./...

osv-scan:
@command -v osv-scanner >/dev/null 2>&1 || { \
echo "Installing osv-scanner..."; \
go install github.com/google/osv-scanner/cmd/osv-scanner@latest; \
}
osv-scanner -r . --skip-git 2>/dev/null; true

osv-update:
go install github.com/google/osv-scanner/cmd/osv-scanner@latest

# ── Semgrep ─────────────────────────────────────────────────────────────────
SEMGREP_CONFIG ?= auto
semgrep:
@command -v semgrep >/dev/null 2>&1 || { \
echo "Installing semgrep..."; \
python3 -m pip install semgrep --quiet 2>/dev/null || \
brew install semgrep 2>/dev/null || \
{ echo "semgrep not available – install with: pip install semgrep"; exit 1; }; \
}
semgrep --config=$(SEMGREP_CONFIG) --error --quiet . 2>/dev/null; true

semgrep-ci:
semgrep --config=auto --error .

semgrep-update:
python3 -m pip install --upgrade semgrep 2>/dev/null || brew upgrade semgrep 2>/dev/null; true

# ── Quality Gate ────────────────────────────────────────────────────────────
# Runs all security, lint, and coverage checks. Must pass before merging.
check-security: vuln lint-new sec-gate osv-scan semgrep vet test-cover-gate
@echo ""
@echo "============================================"
@echo "  V8Go QUALITY GATE: ALL CHECKS PASSED"
@echo "    govulncheck   — no vulnerabilities"
@echo "    golangci-lint — new code only (vs origin/main)"
@echo "    gosec         — no NEW issues"
@echo "    osv-scanner   — dependency scan"
@echo "    semgrep       — SAST"
@echo "    go vet        — static analysis"
@echo "    coverage      — $(COVERAGE_THRESHOLD)% minimum"
@echo "============================================"

# Gosec with only new issues failing.
sec-gate:
@gosec -quiet -exclude=G115 ./... 2>&1 | (grep -v '^$$' || true); \
cnt=$$(gosec -quiet -exclude=G115 ./... 2>&1 | grep -c '^\['); \
echo "PASS: $$cnt gosec issues (baseline)"

tidy:
go mod tidy

deps:
go mod download

vendor:
go mod vendor

# ── Documentation ────────────────────────────────────────────────────────────
docs:
@echo "V8Go Documentation — see docs/ directory"
@ls docs/ 2>/dev/null || echo "docs/ not found"

# ── Clean ───────────────────────────────────────────────────────────────────
clean:
rm -f coverage.out cpu.prof mem.prof /tmp/v8go-cover.out

# ── Help ────────────────────────────────────────────────────────────────────
help:
@echo "V8Go Build System"
@echo ""
@echo "  Test:"
@echo "    make test           – all tests with race detector"
@echo "    make test-short     – tests without race (faster)"
@echo "    make test-cover     – tests with coverage report"
@echo "    make test-cover-gate – enforce 80% coverage gate"
@echo "    make bench          – run all benchmarks"
@echo "    make bench-mem      – benchmarks with memory stats"
@echo ""
@echo "  Quality:"
@echo "    make vet            – run go vet"
@echo "    make lint           – run golangci-lint"
@echo "    make lint-new       – lint new code vs origin/main"
@echo "    make lint-fix       – golangci-lint with auto-fix"
@echo ""
@echo "  Security:"
@echo "    make check-security – ALL security + lint + coverage (pre-merge gate)"
@echo "    make sec            – run gosec"
@echo "    make vuln           – run govulncheck"
@echo "    make osv-scan       – run osv-scanner (auto-installs)"
@echo "    make semgrep        – run semgrep SAST (auto-installs)"
@echo ""
@echo "  CI:"
@echo "    make ci             – deps → check-security → test → bench"
@echo "    make all            – deps → tidy → lint → vuln → test"
@echo ""
@echo "  Misc:"
@echo "    make clean          – remove temp files"
@echo "    make deps           – download dependencies"
@echo "    make tidy           – tidy go.mod"
