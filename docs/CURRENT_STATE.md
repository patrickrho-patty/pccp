# PCCP Current State

**Last updated:** 2026-08-21 — PAT-1445 Router Evolution landed; v2 DoD audit stands at 26/30

## Snapshot

- 6 binaries (`server` · `relay` · `scheduler` · `pia` · `bench` · `alert-backfill`)
- 67 internal packages · 54 console pages · 451 REST routes
- 1,157 tests passing across 57 packages, 0 failures

## What's standing

### Control Plane + consoles
Three-console architecture per PRD v2 (§6): Patty Ops (public service operation),
Enterprise/Government customer console, Account Portal (subscriber self-service).
54 pages over one control API surface.

### DARI data path
Harness traffic is DARI-only (CBOR + COSE-Sign1 over QUIC/TCP). AI semantic v2
(`internal/dari/ai_v2.go`) carries streaming events (§10B.20), tool calling,
structured output, multimodality, and cache accounting. Relay → Scheduler → PIA is
the production hop chain; each hop verifies signed identity before forwarding.

### Model Scheduler (PAT-1445, complete)
- `WorkerFleet`: single worker-state module feeding router, P/D planner, selector, topology
- KV cache routing via the `KVLookup` seam (legacy index + identity-gated WS1 directory)
- Two-stage disaggregated execution (prefill → transfer → decode) with co-located fallback
- Signed envelopes carry governed program metadata (tool-pause state machine end-to-end tested)
- Bounded early rejection: permanent vs transient ineligibility, honest retryable reasons
- Canary controller: shadow → evaluating → active(scope) → paused, evidence-audited transitions
- Region stage: health + preauthorized failover only, hierarchical receipts (`Path{Region,Pool,Worker}`)
- Stage queues with per-stage measurement, output-length uncertainty bands, task-completion SLOs,
  scenario simulation harness running through the real scheduler

### Profile coverage
Public Cloud ops (subscription, work slots, account-integrity/T&S/capacity state separation),
Enterprise governance (DLP, policy epochs, Git-linked line-level provenance), Sovereign profile
(local PKI/KMS, offline catalog). Criterion-by-criterion evidence: [V2_DOD_AUDIT.md](V2_DOD_AUDIT.md).

### Open-source deliverables
`adapters/vllm` · `adapters/sglang`, `sdk/piapi` + examples, `registry/` protocol CSVs,
[DARI.md](../DARI.md) adoption guide.

## Known open items

1. **Legacy HTTP compat path** still exists alongside DARI (permitted by PRD v2 §38.3; flagged
   as non-ideal in DoD audit #24).
2. **SLO alert external routing** (Slack/email/on-call) needs live service configuration;
   framework exists (DoD audit #18/#42-blocked).
3. **Harness program-ID emission** lives in the patty-code repo — relay/scheduler side is done.

## Where to look next

- [V2_DOD_AUDIT.md](V2_DOD_AUDIT.md) — 30-criterion Definition of Done with file-level evidence
- [PAT-1445 Router Evolution](plans/2026-08-20-pat-1445-router-evolution-completion.md) — scheduler design record
- Root [README](../README.md) — architecture map and quick start
