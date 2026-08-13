# 08 — 컴플라이언스 · Compliance (`web/src/pages/Compliance.tsx`)

> Vertical read: component (no `fetch`/`api.` calls) → `api.ts:complianceCerts/complianceAssess` (defined, **unused by the page**) → compliance routes mounted via `setupAdditionalRoutes` → `compliance.GetCertificationPack/AssessCompliance`. Cross-checked the hardcoded control claims against the rest of the audit.

## What this page actually is
The **compliance management** surface — mapping PCCP capabilities to Korean/international frameworks (CSAP, ISMS-P, KISA, Privacy, AI-Basic, ISO 27001/42001) and tracking assessment status + evidence (§41). For regulated buyers this is a primary evaluation page.

## Current vertical — almost entirely fictional
| Layer | Reality |
|---|---|
| Component | **100% client-side hardcoded** — frameworks + full control lists + `status` + `evidence` are `const` arrays in the `.tsx`; `summary`/`complianceScore` computed from them. **No `fetch`, no `api.` call** — `/api/compliance/*` is never invoked. |
| `compliance.AssessCompliance` (110) | itself a **static template** — scores each control off a hardcoded `control.Status` field; identical result for every org; "evidence" is a string, not gathered |
| Control claims | the hardcoded evidence **asserts `compliant` for features the audit shows are missing/fake**: e.g. CSAP-2.1 "조직/부서/프로젝트 계층 RBAC" (RBAC is empty/unwired — Users B5), CSAP-2.2 "위임 관리 + 예외 승인 워크플로" (doesn't exist), ISMS-8.1 "코딩 표준 팩 + 자동 검사" (§33.11 — 0 harness files) |

➡️ This is **phantom compliance** — claiming certification readiness for non-existent capabilities. Forbidden by `MASTER_PLAN.md` §10.8 ("No phantom compliance… Maps and evidence are the product; the certification is the customer's process"). It's the most legally/ commercially dangerous page in the product.

## Gaps — grounded

### A. Make it real (and stop lying)
**A1. Page is static; wire it to the backend.** Replace the hardcoded arrays with `GET /api/compliance/certifications` + `assess`. *Fix:* the service already has the framework shape; surface it.
**A2. Assessment must reflect reality, not a hardcoded status.** *Fix:* derive each control's status from actual PCCP state (does RBAC exist? is audit tamper-evident? is DLP enforced on-path?) via an evidence-gathering engine; mark `gap` honestly where features are missing. Until a control is truly implemented, it must read `gap`/`partial`, never `compliant`.
**A3. Remove fabricated evidence strings** for unimplemented features (A2's honest statuses replace them).

### B. Modeled-but-unwired
**B1. Frameworks exist in the service** (CSAP/KISA/ISMS-P/Privacy/AI-Basic) but aren't surfaced with real depth — no CSAP 간편/일반 distinction, no ISMS-P level (1/2/3), no scope (SaaS/PaaS/IaaS). *(your explicit complaint)* *Fix:* model certification scope + level; let the admin select their target (e.g. CSAP-SaaS 간편) and assess against that specific control set.
**B2. `ControlMapping.EvidenceQuery` exists** — meant to query real evidence — unused. *Fix:* implement evidence queries against audit/provenance/security data.

### C. Genuinely missing
**C1. Evidence vault** (§40.3) — attach/query real evidence per control; download an audit-ready pack.
**C2. Remediation tracking** — gap → task with owner + due date + SLA; today "해결 계획" is a single text field.
**C3. Continuous re-assessment** — schedule re-evaluation as features ship.
**C4. Government overlay** (§41.2) + policy-source model (§41.3).
**C5. Audit-ready export** (ISMS-P/CSAP matrix).

## UX improvements (grounded)
1. **Entire page is static** — reload changes nothing; no persistence (A1).
2. **False `compliant` badges** for missing features (A2/A3) — actively misleading.
3. Read-only — no CRUD on controls/evidence/gaps.
4. No certification/level/scope selector (B1).
5. Summary counts (준수/부분/갭) not clickable to filtered controls.
6. No filter by framework/category/status.
7. No per-control drill-down page; no evidence upload.
8. PRD-ref column not linkable to the spec.
9. "해결 계획" is one text field — no structured task (C2).
10. No favorites; no sub-menu (Frameworks / Evidence / Gaps / Reports).
11. No progress-over-time view; no export (C5).
12. No empty-state; no responsive layout.
13. No indication this is a *self-assessment* vs certified (legal disclaimer missing).
14. No diff when re-assessing after changes.
15. No bulk gap→task conversion.

## Sequencing
Phase 1 (stop the lie — urgent): A1 (wire to API), A2/A3 (honest status from real state; remove fabricated evidence). This page currently creates legal/commercial risk.
Phase 2 (real assessment): B1 (cert level/scope), B2 (evidence queries), C1 (evidence vault), C2 (remediation tasks).
Phase 3 (enterprise): C3 (continuous), C4 (gov overlay), C5 (export pack).
