# 10 — SRE 운영 콘솔 · SREConsole (`web/src/pages/SREConsole.tsx`)  *(Patty Ops)*

> Vertical read: component (4 tabs overview/accounts/capacity/risk) → `/api/public/accounts`, `/api/realtime/status`, `/api/telemetry/snapshot`, `/health` → `publiccloud` Account (4 risk dimensions + slot limits, §10C).

## What this page actually is
The **Public-cloud SRE console** (§7.1, §10C) — service health, the account risk-state dimensions (integrity / T&S / platform-security / capacity), work-slot/capacity, the graduated-response ladder. Patty-internal.

## Current vertical (what exists)
- 4 tabs; reads public `Account` rows (carry the 4 states + slot limits) + realtime/telemetry/health. Account + plan/slots tables render the modeled states.

## Gaps — grounded
**A1. Data is structural, not live-operational.** The 4 states exist on the model but nothing transitions them from live signals (`detection` is dead); `CapacityState` isn't fed by the (dead) scheduler. *Fix:* wire detection→states, scheduler→capacity.
**A2. No real SLO telemetry** (§43) — no burn-rate/error-budget, no TTFT/latency series, no queue depth from admission, no GPU utilization live (`gpuops` has metrics, not live-fed). realtime/telemetry endpoints are thin. *Fix:* live metrics pipeline.
**A3. Integrity vs T&S vs capacity conflated**, not separate panels (§8.9). *Fix:* separate, dedicated panels per dimension.
**A4. Graduated-response ladder (§10C.10)** is a concept, not an actionable workflow. *Fix:* a live ladder with current rung + next action.
**A5. No capacity forecast (§10C.15).** *Fix:* project utilization/queue trends.
**A6. No incident timeline/postmortem (§15.4).** *Fix:* incident lifecycle surface.
**A7. No regional health / dependency status (§7.1).** *Fix:* per-region health + dependency map.
**A8. No alert routing config (§10C.14)** — no Slack/email/on-call. *Fix:* routing + SEV-1/2/3 surface.
**A9. GPU fleet utilization not live (§30.3).** *Fix:* wire `gpuops` metrics live.
**A10. No on-call handoff view.** *Fix:* shift handoff with open incidents/notes.
**A11. No drill-down** — account/state cells aren't clickable to the account or affected sessions/harnesses.
**A12. No server query/wallboard/time-range.**

## UX improvements (grounded)
1. Read-only — nothing drill-downs (A11).
2. Repeated "플랜/하네스 Max" headers — layout confusion.
3. No time-range selector; no auto-refresh toggle/indicator.
4. No wallboard/kiosk mode (§7.5).
5. No historical comparison (§7.6).
6. No favorites/pinned accounts; no export.
7. No sub-menu (capacity/risk/reliability/support).
8. Numbers not clickable to filtered lists (A11).
9. No alert/silence controls (A8).
10. No color-blind-safe legend.
11. No empty-state.
12. ~16 inline literals — verify which are live vs canned.
13. No responsive layout.
14. No live-refresh indicator.

## Sequencing
Phase 1 (usability): drill-downs (A11), time-range, wallboard (§7.5), layout fixes, server query.
Phase 2 (real telemetry): A1 (detection→states, scheduler→capacity), A2 (SLO/latency/queue), A9 (GPU live).
Phase 3 (response): A4/A5/A6/A7/A8 (ladder/forecast/incident/regional/alerts), A10 (on-call).
