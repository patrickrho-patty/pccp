# 11 — 서비스 커맨드 센터 · ServiceCommandCenter (`web/src/pages/ServiceCommandCenter.tsx`)  *(Patty Ops)*

> Vertical read: component → `/api/dashboard` (real DB counts), `/api/public/accounts`, `/health`, `/api/realtime/status`, `/api/telemetry/snapshot` → `handleDashboard` (live counts).

## What this page actually is
The **Patty Ops landing/command center** for the public service (§6.1) — at-a-glance overview + entry to live-traffic/accounts/models/capacity/risk/reliability/support areas.

## Current vertical (what exists)
- Reads `/api/dashboard` (real counts: users/harnesses/sessions/endpoints + active sessions), public accounts, health, realtime, telemetry. Stat cards + subscriber table (plan/integrity/T&S/capacity states).

## Gaps — grounded
**A1. Live traffic view missing** (§6.1) — only counts, no active accounts/harnesses/sessions/slots/queues/PAPER-exchanges view. *Fix:* live-traffic panel.
**A2. Account-integrity / T&S / capacity not shown live** (§8.9) — states are static. *Fix:* live state per dimension (depends on detection/scheduler wiring).
**A3. Work-slot & queue health** from the (dead) scheduler — absent. *Fix:* wire scheduler.
**A4. Capacity-lease issuer/validator not live.** *Fix:* live lease state.
**A5. No support timeline per account** (§6.1 Support). *Fix:* support case timeline.
**A6. No refund/entitlement escalation** (§6.1). *Fix:* escalation workflow.
**A7. No regional health** (§7.1). *Fix:* regional status.
**A8. No incident/command panel.** *Fix:* incident command surface.
**A9. No Trust & Safety case queue** (§10C.11) — separated from integrity. *Fix:* T&S case queue.
**A10. No abuse signal feed** (§10C.9). *Fix:* abuse signals (depends on detection).
**A11. Counts not actionable** — stat cards not clickable; no drill-down to records.
**A12. No server query/wallboard/time-range; realtime/telemetry thin.**

## UX improvements (grounded)
1. Stat cards not clickable (A11).
2. Subscriber cells (integrity/T&S/capacity) not clickable.
3. No time-range; no wallboard (§7.5); no auto-refresh indicator.
4. No search across accounts; no filter by plan/status/risk.
5. No sub-menu matching §6.1 nav tree (Live Traffic/Accounts/Models/Capacity/Risk/Reliability/Support).
6. No export; no favorites; no alerts/incidents panel.
7. No empty-state; no responsive layout.
8. Plan/sub status not filterable.
9. ~6 inline literals — verify live vs canned.
10. No drill-down anywhere (A11).
11. No notification center.
12. No loading skeleton.
13. No responsive reflow.
14. No favorites/pinning.

## Sequencing
Phase 1 (usability): clickable stats/drill-downs (A11), time-range, wallboard, search/filter, sub-menu.
Phase 2 (depth): A1 (live traffic), A3/A4 (scheduler/leases), A2/A9/A10 (live states/T&S/abuse), A7 (regional).
Phase 3 (ops): A5/A6/A8 (support/refund/incident).
