# 09 — 플릿 관리 · Fleet (`web/src/pages/Fleet.tsx`)

> Vertical read: component → `fetch /api/fleet/{inventory,actions,sessions/{id}/inspect}` + `/api/audit` → `handleFleetInventory/handleFleetAction/handleInspectSession` → `fleet.GetFleetInventory/PerformAction/InspectSession`.

## What this page actually is
**Live fleet operations** (§14.2 fleet actions, §14.3 session inspector) — the SOC/SRE surface for acting on the harness fleet and inspecting live sessions. Where containment happens.

## Current vertical (what exists) — mostly real
- Inventory table (harness/user/status/risk/sessions/approvals/findings/actions), action panel with **required reason**, session inspector modal, action history (from `/api/audit`).
- `fleet.PerformAction` is **real** — revoke/quarantine set status+risk + `revokeAllSessions`; terminate-session; suspend-model revokes leases; emergency-lockdown terminates all org sessions + sets all harnesses high-risk.

## Gaps — grounded
**A1. Actions are DB-only; not live-propagated.** Quarantine/terminate/lockdown change DB rows but the relay doesn't enforce state on the live path (Domain 1–2) — a "terminated" session's stream keeps going. *Fix:* propagate fleet state to the relay (kill live exchanges).
**A2. No live containment verification.** Can't confirm a harness actually dropped. *Fix:* relay ack + connection-state feedback after an action.
**A3. Change-freeze activation per repo/branch (§33.13)** — lives in `korean`, not surfaced/triggerable here. *Fix:* a freeze action scoped to repo/branch.
**A4. Force-harness-version block (§33.10)** — `korean.SetForcedHarnessVersion` exists; not actionable from fleet. *Fix:* block/select by version/ring.
**A5. No mass-action targeting** (by risk/version/affiliation). *Fix:* bulk act on filtered selection.
**A6. No per-harness action history** (only generic `/api/audit`). *Fix:* a dedicated fleet-action log per harness with revert where possible.
**A7. No forensic-snapshot download** (`/api/sandboxes/{id}/snapshot` exists elsewhere; §40.3). *Fix:* evidence-bundle download from a harness/session.
**A8. Quarantine network-isolation not confirmed.** *Fix:* verify the harness's network grants revoked.
**A9. No approvals-queue integration.** *Fix:* surface pending approvals reachable from fleet.
**A10. No broadcast-to-affected-users on lockdown (§22).** *Fix:* notify affected developers/owners when locking down.
**A11. Emergency lockdown is one-click** (`executeAction('', 'emergency_lockdown', …)`) — catastrophic, no 2-step/scope. *Fix:* mandatory 2-step confirm + scope (org/project/selected) + impact preview.
**A12. No server-side filter/pagination; no live-refresh indicator.**

## UX improvements (grounded)
1. Emergency lockdown one-click (A11) — 2-step + scope + impact.
2. Reason field inconsistent (required for some, not others) — require for all destructive.
3. No filter by risk/status/version/user (A4/A5).
4. Sessions/approvals/findings columns are counts, not clickable to filtered lists.
5. Harness cell not deep-linkable to harness detail.
6. No bulk select for mass actions (A5).
7. No live/auto-refresh indicator (A12).
8. No favorites/pinned harnesses; no sub-menu (live/quarantined/history).
9. No animation on state change; no per-row action timeline (A6).
10. Action result not shown inline (no success/fail toast).
11. Session inspector is a non-deep-linkable modal (Sessions B5).
12. No empty-state; no keyboard shortcuts.
13. No export of action history.
14. No "affected N harnesses/sessions" preview before a fleet-wide action.
15. No confirmation animations for destructive ops.

## Sequencing
Phase 1 (safety): A11 (lockdown 2-step+scope), require-reason consistency, inline results, server query.
Phase 2 (real enforcement): A1/A2 (live propagation + verification) — depends on protocol-core rebuild.
Phase 3 (enterprise): A3/A4/A5 (freeze/force-version/mass), A6/A7/A8/A9/A10 (history/snapshot/isolation/approvals/broadcast).
