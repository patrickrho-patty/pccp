# Harness E — Operational & Sovereign (`patty-code-pccp`)

> Grounded in: PCCP `internal/publiccloud` (WorkSlot/CapacityLease — real, but not enforced on path; scheduler dead), `internal/realtime` (SSE/WS hub — exists, **not mounted/fed**), `internal/sandbox` (definition-only, no runtime), `internal/sovereign` (offline-update *"would apply"* stub), and the harness's `sandbox`/`provider`/agent surfaces.

## What exists (honest baseline)
- The harness is a full local agent (worktree, sandbox config, hooks, telemetry, crashreport, profile).
- PCCP models capacity/fair-use, comms, sovereign updates — but most are stubs or unwired to the live path.

## The gaps

### E1. Quota / fair-use / capacity awareness in-IDE (§10C.3/§10C.7)
- The dev sees nothing about their work-slot class, queue position, or capacity-lease state — "why am I throttled?" is opaque. *Fix:* harness fetches + displays slot/queue/lease state from PCCP (depends on wiring `RecordUsage` + the scheduler on the path).

### E2. Comms hub delivered in-IDE over DARI (§21.2/§22.4)
- Comms is CP-side only (web 13); the harness never receives chat/presence/broadcast/admin-commands. *Fix:* a DARI comms channel to enrolled harnesses; admin commands execute + emit audit.

### E3. Sovereign / air-gap operation (§34.5)
- PCCP `sovereign.ApplyUpdate` is a *"would apply"* stub; the harness has no offline catalog/trust-source/signed-advisory handling. *Fix:* offline catalog, local trust sources, signed offline advisories (recall/policy), update bundles that install without public internet; harness boots/operates fully offline in government builds.

### E4. Sandbox-baseline enforcement (§31.2)
- PCCP sandbox is definition-only (web 15); the harness `sandbox` is local config. *Fix:* remote-sandbox as the enforced baseline for sensitive repos; local execution an explicit, audited exception (§31.4).

### E5. Admin-command channel with full audit (§22.4/§14.2)
- PCCP fleet actions change DB but never reach the live harness. *Fix:* governed admin commands (lock/freeze-branch/force-update/recall) delivered to the harness, executed, evidence emitted (ties to Harness A DARI records + B evidence).

### E6. Employment-decision guardrails on the client (§26/§27)
- The harness must not surface raw-activity surveillance to managers; Work-Intel signals are aggregated/signed/human-finalized — the client respects the same boundary as the console.

## Implementation notes
- E1/E2 depend on the protocol core (Harness A) + wiring metering/realtime on PCCP. E3 is largely harness-side offline packaging + PCCP sovereign-update completion. E5 depends on A (command records) + B (audit emission).

## Acceptance
- A dev sees their slot/queue state; comms/broadcasts arrive in the harness; a government build operates offline with signed updates; sensitive repos force sandbox execution; an admin lockdown reaches the live harness and is audited.
