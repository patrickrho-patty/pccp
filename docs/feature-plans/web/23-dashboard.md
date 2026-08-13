# 23 — 대시보드 · Dashboard (`web/src/pages/Dashboard.tsx`)

> Vertical read: component → `/api/dashboard` (real DB counts), `/api/korean/governance-brief`, `/api/security/findings` → `handleDashboard` (live counts). Cross-checked the in-page demo-seed button.

## What this page actually is
The **Enterprise/Gov overview dashboard** (§7.3) — counts, active sessions, recent findings, governance brief; the admin landing page.

## Current vertical (what exists)
- Reads `/api/dashboard` (real counts + active sessions), governance brief, findings. Stat cards, active-session list, quick actions. A **"📊 데모 데이터 생성" button** calls multiple seed endpoints.

## Gaps — grounded
**A1. Demo-seed button ships in the UI** *(your complaint)* — data-fabrication in prod is a trust/audit problem. *Fix:* gate behind dev/non-empty or remove.
**A2. Stats aren't clickable** to underlying lists *(your example — every number should drill down)*. *Fix:* `<StatCard to=…>`.
**A3. No role/persona scoping** (§5, §6.5) — every operator sees the same dashboard regardless of role/clearance. *Fix:* role/persona-scoped widgets.
**A4. Governance brief widget** (§33.12) — fetched but make it a rich, drillable widget.
**A5. No active-incidents / open-gaps widget.** *Fix:* surface incidents + compliance gaps.
**A6. No quick-action wizards** (enroll harness, create project). *Fix:* wizard entry points.
**A7. No recently-viewed / favorites.** *Fix:* recents + pinned entities.
**A8. No cross-object navigation** (§6.3). *Fix:* object hub.
**A9. No notification center.** *Fix:* notifications/alerts.
**A10. No onboarding checklist for new admins; no KPI-vs-target.**
**A11. No time-range.**

## UX improvements (grounded)
1. Demo-seed button in prod (A1).
2. Stat cards not clickable (A2).
3. "전체 보기 →" links generic (A8).
4. No time-range (A11).
5. No role-scoped view (A3).
6. No animation/transitions; no loading skeleton.
7. No favorites/pinning; cards not reorderable.
8. No sub-menu; no theme/density toggle.
9. No empty-state/onboarding (A10); no notification center (A9).
10. No recent-activity personalization (A7).
11. No responsive reflow.
12. No KPI-vs-target (A10).
13. No quick-action wizards (A6).
14. No active-incidents widget (A5).

## Sequencing
Phase 1 (trust + usability): remove/gate demo-seed (A1), clickable stats (A2), role scoping (A3), time-range (A11), cross-object nav (A8).
Phase 2 (engagement): notification center (A9), onboarding checklist + KPI (A10), favorites/recents (A7), quick-action wizards (A6), incidents widget (A5).
Phase 3 (polish): animations, theme/density, reorderable cards, responsive.
