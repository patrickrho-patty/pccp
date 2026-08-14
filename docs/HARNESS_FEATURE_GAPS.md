# Patty Code Harness — Critical Missing Features (Enterprise / Government / Governance Positioning)

**Generated:** 2026-08-13
**Harness location:** `patty-code-pccp/` (Go agent + desktop shell)
**Grounding:** verified against actual harness code. Honest baseline first — the harness is a **mature local agent**: `capability` (2K LOC ledger), `evidence` (5.7K), `control` (37K), `permission` (3.1K), `guardian` (1.5K), `hook` (6K), plus `worktree`, `sandbox`, `secrets`, `checkpoint`, `trajectory`, and a real `internal/provider/dari` + vendored `dariproto`. The foundation is strong.

**The gap is the PCCP integration layer.** Eight governance categories return **zero files** in the harness: policy-epoch binding, DLP/PII/injection, server model-catalog, change-freeze, coding-standard packs, mandatory acknowledgement, forced-version/ring, and model recall. Evidence is **local-only** — nothing streams to the control plane. A governed enterprise harness is the whole point of our positioning; these are the critical features missing.

---

## A. Trust & transport (the harness must be a governed DARI peer, not an OpenAI client)

**1. DARI-only enforcement with OpenAI/Anthropic path removed by default.**
Today `internal/provider/{openai,anthropic}` coexist with `paper`. Per §0.2/§9.2 and your explicit requirement, the harness must **refuse** non-DARI endpoints in enterprise/gov/public builds (dev fallback only, feature-flagged). Add a build/deployment lock that errors out if `provider != paper` in non-dev profiles.

**2. Full DARI enrollment & capability-lease lifecycle.**
`workspacelease` exists, but enrollment (cert issuance from PCCP CA), lease acquisition, renewal, and **fail-closed on expiry** must be first-class: the harness cannot start a protected session without a valid lease, and must gracefully stop (not silently fall back) when a lease expires mid-session (§8.4).

**3. Hardware/runtime attestation at enrollment.**
The harness should attest its runtime (binary signature, build hash, OS, sandbox posture) to PCCP at enrollment and re-attest on change — the client-side counterpart to PIA attestation (§9.6). Today `appidentity` exists but no attestation handshake.

**4. Policy-epoch binding & stale-epoch refusal.**
Zero files today. The harness must fetch and **bind** to the active policy epoch, refuse to act if its epoch is stale/revoked, and re-bind on epoch change (§13.1, §10A.5). This is what makes "governance before routing" real on the client.

**5. Server-authoritative model catalog consumption.**
Zero files. The harness must take its model list **from PCCP over DARI** (`dari.models/1`), not from local `patty.toml` defaults (§10A, §0.1). Local config becomes connection/identity only. This kills the "user types a fake model ID" problem at the source.

---

## B. Provenance & evidence (the reason enterprises trust us)

**6. Stream provenance to the control plane — not local-only.**
`evidence` is rich but emits nothing to PCCP (verified: no controlplane/relay/paper emission). The harness must record `ChangeSet` + line-level `ProvenanceSpan` + `ActionEnvelope` and push them to the Relay so an admin clicking code sees real, live attribution (§19, §40.3). Without this, the entire provenance product is empty.

**7. Content-addressed, rename-safe provenance.**
Spans must survive file rename/move/repo evolution (§19.2, §19.5) — keyed by content hash + AST anchor, not brittle line numbers. Add a survival test that moves a file and re-resolves attribution.

**8. Evidence-receipt acknowledgment (tamper-evident loop).**
Every protected action should produce a signed evidence receipt that PCCP acknowledges back; the harness retains the ack as tamper-evidence (§40.3, §39.6). Closes the loop the Relay's `CloseExchange` was meant to.

**9. Prompt → tool → file → commit attribution chain in the harness UI.**
Developers should see, per edit, *which prompt produced this code, on which model, approved by whom* — the same chain the admin sees, from the author's side (§19.1). Builds trust and self-correction.

**10. Replay/reproduce a change from provenance.**
Given a span, the harness can reconstruct and replay the exact context+model to reproduce an edit (§19.1, §14.3) — critical for audit disputes and code review.

---

## C. Inline security governance (enforce before the byte leaves the machine)

**11. Client-side DLP / Korean-PII / secret redaction before send.**
Zero files. The harness must scan outgoing context for Korean PII (RRN, 계좌, 카드, 사업자번호) and secrets **before** DARI dispatch, with policy-driven block/redact/allow (§16.3, §16.5). Relying on the Relay alone is too late and too coarse.

**12. Prompt-injection defense at the harness boundary.**
Guard against indirect injection from repo content / tool output / MCP (§16.4, §35.8): treat untrusted content as data, detect override/jailbreak patterns, and refuse to act on injected instructions. `guardian` is the natural home.

**13. Tool/MCP approval workflow against PCCP.**
Tools and MCP servers must be checked against the PCCP tool registry/approval state before invocation; unapproved tools are blocked, not just logged (§17.1, §17.2). `mcpregistry`/`mcplaunch` exist locally; wire them to the CP approval gate.

**14. Network broker & secret broker integration.**
Outbound network calls and secret access must go through CP-issued grants (§17.4, §17.5) — the harness asks the broker, never hardcodes keys/URLs. `secrets` exists locally; make it a CP-brokered consumer.

---

## D. Enterprise workflow governance (the Korean differentiators, client-side)

**15. Mandatory policy-acknowledgement gate.**
Zero files. When PCCP changes policy materially, the harness blocks configured high-risk workflows until the user acknowledges (§33.6) — surfaced in-IDE with the Korean summary.

**16. Change-control-board submission for high-risk AI patches.**
Zero files. The harness auto-submits high-risk changes (crypto/payment/PII-adjacent, new MCP, new network dest, new dependency) to the CP change-control queue and blocks merge until approved (§33.4).

**17. Change-freeze / critical-period compliance.**
Zero files. The harness must respect active freezes on protected branches — allow read/review/test, block AI write/export, require elevated approval (§33.13). Surfaced clearly to the dev so it's not a mystery failure.

**18. Coding-standard & architecture packs.**
Zero files. Enforce org coding standards (framework versions, package boundaries, forbidden deps, naming/API conventions) with **plain-Korean explanations** when a rule blocks work (§33.11). Maps to `guardian`/`hook`.

**19. Forced harness version & release-ring enforcement.**
Zero files. The harness checks its own build against the CP-declared minimum version/ring and self-updates or refuses to start if below minimum / on a blocked build (§33.10).

**20. Emergency model-recall compliance.**
Zero files. On a signed recall advisory from PCCP, the harness suspends the recalled model, surfaces affected sessions/commits, and offers the declared replacement (§33.9).

---

## E. Operational & sovereign features

**21. Lease/quota/fair-use awareness in the UI.**
The harness should show the developer their work-slot class, queue position, and remaining capacity lease — so "why am I slow/throttled" is explainable, not opaque (§10C.3, §10C.7).

**22. Comms hub delivered in-IDE over DARI.**
Presence, 1:1/chat, broadcasts, file handoff, and **admin commands** delivered to the harness (§21.2, §22.4) — today comms is CP-side only. The harness is where the developer actually is.

**23. Sovereign / air-gap operation.**
Offline catalog, local trust sources, signed offline advisories (recall/policy), and update bundles that install without public internet (§34.5). The harness must boot and operate fully offline in government builds.

**24. Sandbox-baseline enforcement.**
`sandbox` exists; make remote-sandbox the enforced baseline for sensitive repos (§31.2), with local-execution as an explicit, audited exception — not the default.

**25. Admin-command channel with full audit.**
PCCP admins must be able to issue governed commands to a harness (lock, freeze-branch, force-update, recall) that execute **and** emit audit evidence (§22.4, §14.2). Today fleet actions change DB state but never reach the live harness.

**26. Employment-decision guardrails on the client.**
The harness must not surface raw-activity surveillance to managers; Work-Intel signals are aggregated, signed, and human-finalized — the client respects the same boundary as the console (§26, §27).

---

## Priority call
The single highest-leverage cluster is **A + B** (DARI-only + provenance streaming + catalog/epoch binding): it's what converts this from "a good local agent" into "a governed Patty Code harness," and it's the precondition for nearly every enterprise/gov sale. C (inline DLP/injection) and D (the Korean differentiators) are what then differentiate us from OpenAI/Anthropic/Cursor-style tools. E rounds out operability.

---

## Confidence
Findings verified by package presence + targeted grep (eight governance categories = 0 files; evidence = no CP emission; provider = openai/anthropic/paper coexist). Internal logic of `control`/`evidence`/`guardian` was not line-audited — some capabilities may partially exist under different names; a deeper harness pass can confirm before implementation.
