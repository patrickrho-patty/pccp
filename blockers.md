# PCCP Implementation Blockers

Blockers encountered during plan execution, with workaround/decision and resolution status.

| # | Date | Sub-project | Blocker | Decision / Workaround | Status |
|---|------|-------------|---------|----------------------|--------|
| 1 | 2026-08-15 | S2 (all later suites) | Pre-existing flake: `internal/audit` chain tests failed under `-count=N` because the chain allocator caches per-org high-water state process-wide, and fixed org IDs leaked previous runs' chains into fresh DBs; SQLITE_BUSY retries also burned allocator sequences. | Fixed in tests: unique org per run + `_busy_timeout`/WAL DSN. Verified 10× green. | Resolved |
| 2 | 2026-08-15 | all suites | `internal/sandbox` TestDockerRuntimeWhenAvailable flakes under full-suite parallel load (Docker daemon pull contention). Passes in isolation; environment-dependent, not a code path in the plans. | Left as-is; re-run isolated when it fails. Docker is not exercised by the S2–S12 scheduler work. | Open (environmental) |
