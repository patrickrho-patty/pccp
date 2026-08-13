# 22 — 엔터프라이즈 하네스 기능 · EnterpriseFeatures (`web/src/pages/EnterpriseFeatures.tsx`)

> Vertical read: component → `/api/enterprise/{features,violations,features/seed,features/{id},violations/{id}}` → handlers (2368–2410) → `models/enterprise.go EnterpriseHarnessFeature/EnterpriseFeatureViolation`. Seed list inspected.

## What this page actually is
A **tracker for enterprise-only harness capabilities** (§33) — the catalog of governance/security/compliance features the harness reports to PCCP, their enabled/enforced state, last-reported value, and violations. Enterprise/gov-only (not public). The admin side of the "20 business-specific harness features" (§33).

## Current vertical (what exists)
- 2 tabs (features/violations); seed ("20개 기본 기능 등록"), toggle enabled, resolve violation.
- `EnterpriseHarnessFeature` model: `FeatureKey`, `Category`, `Enabled`/`Enforced`, `Status`, **`LastReportedAt`/`LastValue`** (harness-reported), `ViolationCount`, `Config`.
- Seed = static catalog mapping to §33/§17/§31 (code_review §33.4, code_signing §18.6, coding_standards §33.11, sandbox_execution §31.2, secret_broker §17.5, …).

## Gaps — grounded
**A. Features reference mostly-unimplemented capabilities.** coding_standards (§33.11 — 0 harness files), sandbox_execution (§31.2 — fake runtime), exception_workflow (§33.8 — missing), etc. Tracking them as `active/enforced` is phantom (same risk as Compliance). *Fix:* status must reflect real implementation; mark `planned` until the capability + harness reporting exist.
**B. Harness never reports.** `LastReportedAt`/`LastValue`/violations are designed for harness-reported telemetry that doesn't flow (HARNESS D). *Fix:* harness emits feature-status + violation events over PAPER; PCCP records them.
**C. `Enforced` flag is inert** — nothing blocks work when an enforced feature is absent (no harness-side gate). 
**D. Seed may add duplicates on repeat** (no dedupe shown — verify); no per-org enable/disable by affiliate; no rollout rings.

## UX improvements (grounded)
1. Features shown `active/enforced` for unimplemented capabilities (A) — misleading.
2. Read-mostly — toggling enabled/enforced does nothing real (C).
3. No filter by category/severity/status; no detail page.
4. "해결" violation action has no workflow/evidence.
5. PRD ref not linkable; no search.
6. No favorites; no sub-menu by category; no export.
7. No empty-state; seed gives no feedback (dupes?).
8. Timestamps raw; no responsive layout.
9. No indication these await harness reporting (B).

## Intended-features coverage (vs WEB_FEATURE_GAPS §23 — 10 features)
1. Make the 20 features real/configurable/enforced → **A/B/C** ✅
2. Per-feature enable/disable by org/affiliate → **D** (per-affiliate) ✅
3. Severity → routing → owner workflow → **add**; routing/owner workflow.
4. Tie each feature to a harness capability + PAPER message → **B** ✅
5. Compliance/evidence per feature → **add**; per-feature evidence.
6. Audit of feature toggles → **add**; toggle audit.
7. Rollout rings (§33.10) → **D** ✅
8. Feature dependency graph → **add**; dependency graph view.
9. Bulk import/export feature packs → **add**; import/export.
10. Health/usage of each feature → **add**; per-feature health/usage (from harness reporting, **B**).

## Sequencing
Phase 1 (honesty): A (status reflects reality — mark `planned` where unimplemented), dedupe seed + feedback.
Phase 2 (real): B (harness reports feature status/violations), C (harness enforces `Enforced` gates).
Phase 3 (ops): per-affiliate enable, rollout rings, detail pages, filters.
