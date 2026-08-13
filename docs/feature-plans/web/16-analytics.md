# 16 — 분석 · Analytics (`web/src/pages/Analytics.tsx`)

> Vertical read: component → `fetch /api/analytics/{usage,engineering,security}` + `/api/korean/governance-brief` → `server.go` analytics handlers → `workintel.GetUsageSummary/GetEngineeringMetrics/GetSecurityMetrics`. Cross-checked the data source (`UsageRecord`).

## What this page actually is
**Engineering/AI-use analytics** (§28, §29) — token usage by model/user, engineering metrics, security posture, and an executive governance brief. The analytics rollup over governed activity.

## Current vertical (what exists)
- Fetches 4 endpoints; renders CSS bar charts (`BarGroup`) + a governance brief; CSV export.
- `GetUsageSummary` is **real** — aggregates `UsageRecord` (tokens_in/out by model + user + day).
- `GetEngineeringMetrics`/`GetSecurityMetrics`/scorecard real (compute over DB).

## Gaps — grounded
**A. Data depends on `RecordUsage` being wired — it isn't.** Nothing on the live inference path writes `UsageRecord` (MISSING_ITEMS P0), so analytics shows empty/seed data. *Fix:* wire metering (Relay stage 13) — then analytics reflects reality.
**B. No number is clickable** *(your example)* — token totals, model/user breakdowns are static; can't drill to the underlying records. *Fix:* every stat → filtered list (sessions/exchanges/users).
**C. No time-range / granularity / comparison** (§7.6) — fixed 30-day window.
**D. CSS bar charts aren't interactive** — no hover detail, zoom, filter, legend toggle.
**E. No cost/KRW** (§29.9), no cohort analysis (§28.2), no NL analytics (§28.4), no scheduled report delivery (§45.2), no saved per-role dashboards (§28.3).

## UX improvements (grounded)
1. Stats not drill-downable (B).
2. No time-range selector / granularity (day/week/month) (C).
3. No comparison periods (§7.6).
4. Charts non-interactive (no hover/zoom/filter) (D).
5. No favorites/saved views; no sub-menu (usage/cost/engineering/adoption).
6. CSV export only — no PDF, no scheduled delivery.
7. No loading skeleton; no empty-state ("analytics appear once sessions run").
8. No responsive chart sizing; no tooltips/legend toggle.
9. Numbers not localized consistently; no shareable link to a view.
10. No anomaly annotation on the timeline.

## Intended-features coverage (vs WEB_FEATURE_GAPS §14 — 10 features)
1. Numbers clickable to records → **B** ✅
2. Cohort analysis (§28.2) → folded P3; **add** cohort breakdown.
3. Cost breakdown by model/project/user + chargeback export (§29.12) → **E** (cost/KRW) ✅
4. Anomaly detection on usage spikes → folded P3; **add** anomaly alerts.
5. NL analytics / ask-a-question (§28.4) → **E** ✅
6. Saved custom dashboards per role → **E** ✅
7. Scheduled report delivery (§45.2) → **E** ✅
8. Export to BI (CSV/JSON connector) → partial (CSV); **add** BI connector.
9. KPI scorecards vs targets (§50) → **E** ✅
10. Trend + forecast → **E** ✅

## Sequencing
Phase 1 (depends on P0): wire `RecordUsage` (A) — without it the page is empty.
Phase 2 (usability): drill-down (B), time-range/comparison (C), interactive charts (D).
Phase 3 (advanced): cost/KRW, cohorts, NL analytics, scheduled reports, role dashboards.
