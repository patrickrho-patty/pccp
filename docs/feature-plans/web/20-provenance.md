# 20 — 프로바이던스 체인 · Provenance (`web/src/pages/Provenance.tsx`)

> Vertical read: component → `api.getProvenance(sessionId)` → `handleGetProvenance` → `provenance.GetProvenanceChain`. Companion to CodeExplorer (19) — the **per-session** chain reached from Sessions (`/sessions/:id/provenance`).

## What this page actually is
The **per-session provenance chain** (§19.5) — for one session, the full user→harness→prompt→model→file→diff→policy→commit lineage + action timeline.

## Current vertical (what exists)
- Fetches `GetProvenanceChain(sessionID)`; renders session info + action timeline. Minimal (back button + lists).
- `ActionEnvelope` actions are recorded (relay `RecordAction`), so the timeline can have data; but `ChangeSet`/`ProvenanceSpan` are never created (CodeExplorer A), so the code-level chain is absent.

## Gaps — grounded
**A1. Chain is action-only, not code-level.** No changesets/spans/diff/commit binding shown (depends on CodeExplorer A — harness must emit changes). *Fix:* include changesets, line spans, commit bindings, evidence receipts in the chain.
**A2. No evidence-receipt verification** (§40.3) — `EvidenceReceipt` exists; not shown/verified. *Fix:* receipt tab with signature verification status.
**A3. No export chain as signed bundle.** *Fix:* export the full chain as a signed, tamper-evident bundle.
**A4. No replay-any-node** (§14.3). *Fix:* replay an action/exchange through the governed pipeline.
**A5. No cross-session provenance search.** *Fix:* search spans/changesets across sessions.
**A6. No privacy-level gating on displayed content** (§27). *Fix:* gate content by caller visibility level.
**A7. No survives-rename/repo-evolution test view** (§19). *Fix:* a view proving spans resolve after rename/move.
**A8. No timeline scrubber** for long sessions.
**A9. No policy-decision annotation per action** (§13). *Fix:* show the allow/deny decision + rule per action.
**A10. No link to compliance evidence** (§41). *Fix:* tie actions/changes to compliance controls.

## UX improvements (grounded)
1. Chain shows actions only, not code (A1).
2. Minimal page — no search/filter; no export (A3); no shareable link.
3. No drill-down on action payload; no JSON/raw toggle.
4. No visual graph of the chain.
5. Timestamps raw; IDs not copyable.
6. No evidence-receipt tab (A2).
7. No timeline scrubber (A8); no legend.
8. No favorites; no sub-menu; no empty-state; no responsive.
9. No diff view for changesets.
10. No policy-decision annotations (A9).
11. No replay action (A4).
12. No back-to-session deep link context.
13. No keyboard nav.
14. No loading skeleton.

## Sequencing
Phase 1 (completeness): A1 (full chain incl. changesets/spans/commits/receipts) — depends on CodeExplorer A; A2 (receipts); A9 (policy annotations).
Phase 2 (trust/usability): A3 (signed export), A4 (replay), A5 (cross-session search), A6 (privacy gating), visual graph, scrubber.
Phase 3 (enterprise): A7 (rename-survival view), A10 (compliance links).
