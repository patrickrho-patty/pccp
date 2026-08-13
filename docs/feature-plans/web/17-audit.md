# 17 — 감사 로그 · Audit (`web/src/pages/Audit.tsx`)

> Vertical read: component → `fetch /api/audit` → `handleListAuditEvents` (org-scoped, limit 200) → `models/provenance.go AuditEvent` (has `LegalHold`, `EventDigest`).

## What this page actually is
**Audit, evidence, retention, and legal hold** (§40, DoD #13) — the tamper-evident record of who did what, when, on what; the bedrock for compliance and incident investigation.

## Current vertical (what exists)
- `handleListAuditEvents`: org-scoped, ordered DESC, **limit 200**.
- `AuditEvent` model: `EventType`, `ActorID/Type`, `Action`, `ResourceType/ID`, `Details` (JSON), IP, UA, `Result`, **`LegalHold`**, **`EventDigest`**, `OccurredAt`.
- Component: FilterBar with time presets (오늘/어제/7일/30일/전체), stats summary, CSV + PDF (print) export.

## Gaps — grounded
**A. Hard 200-row cap + client-side filter.** *Fix:* server-side pagination/filter (`?page=&type=&actor=&resource=&result=&from=&to=`); current 200-cap hides older events.
**B. Tamper-evidence unused.** `EventDigest` exists but the page never verifies the hash chain (§39.6) — an admin can't confirm the log is intact. *Fix:* a "verify chain" action + per-event signature status.
**C. Legal hold is a field, not a workflow.** Can't set/scoped-hold events from here (§40.5). *Fix:* place/lift legal hold on events/resources with reason + audit.
**D. No drill-down to full payload.** `Details` JSON isn't expandable; no raw/JSON toggle.
**E. No SIEM forwarding** (§32.4), no live tail/streaming, no retention-class display/enforcement (§40.4), no evidence-bundle assembly from selected events (§40.3).

## UX improvements (grounded)
1. 200-row cap (A) — older audit history inaccessible.
2. Client-side filter/pagination (A).
3. Rows not expandable to full payload (D); no JSON/raw toggle.
4. No verify-chain action (B).
5. No legal-hold workflow (C).
6. Stats (total/success/denied) not clickable to filtered list.
7. No live-refresh toggle; no streaming tail.
8. No actor/resource partial search; presets exist but type/result filters limited.
9. No favorites/saved queries; no sub-menu (admin/model/security/system).
10. Export without column selection.
11. No relative-time column (absolute only); no color legend for Result.
12. No keyboard nav; no empty-state.
13. No diff view for config-change events.
14. No compliance-scoped views (per certification).

## Intended-features coverage (vs WEB_FEATURE_GAPS §15 — 10 features)
1. Legal-hold flagging per event (§40.5) → **C** ✅
2. Retention-class display + purge schedule (§40.4) → **E** ✅
3. Tamper-evidence / hash-chain verify UI (§39.6) → **B** ✅
4. Admin-action audit specifically (§6.5, DoD #13) → **E** (admin-scoped view) ✅
5. Evidence-bundle assembly from selected events (§40.3) → **E** ✅
6. Saved searches / watchlists → **add**; saved-query feature.
7. Streaming tail (live audit) → **E** ✅
8. SIEM forwarding (§32.4) → **E** ✅
9. Actor correlation graph → **add**; actor/event correlation view.
10. Compliance-scoped views (per certification) → **E** ✅

## Sequencing
Phase 1 (usability): server query (A), row expand/JSON (D), clickable stats, partial search.
Phase 2 (trust): verify-chain UI (B), legal-hold workflow (C), evidence-bundle assembly.
Phase 3 (enterprise): SIEM forwarding, live tail, retention classes, compliance views.
