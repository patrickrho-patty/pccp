# 07 — 보안 운영 센터 · Security (`web/src/pages/Security.tsx`)

> Vertical read: component → `api.ts` + direct `fetch` → `server.go` security handlers (1900–2040) → `security.CheckContext/detect*/RecordFinding` → `models/tool_runtime.go SecurityFinding`. Cross-checked against the live path (`relay/paper_listener.go`).

## What this page actually is
The **Security Operations Center** — DLP/PII/secrets/injection detection, the rule catalog, recorded findings (with session/user/harness correlation), a manual scanner, and emergency lockdown (§15, §16, §35). It's the SOC analyst's primary surface.

## Current vertical (what exists) — mixed real/fake
| Layer | Reality |
|---|---|
| Component | 4 tabs (dashboard / rules / findings / scanner); rule builder; finding detail + resolve; scanner textarea; lockdown button |
| **Scanner** (`handleSecurityCheck`) | **REAL** — runs `security.CheckContext` → `detectKoreanPII`/`detectSecrets`/`detectInjection`/`detectSensitivePaths` with redaction |
| **Findings** (`handleSecurityFindings`/`Detail`/`Update`) | **REAL** — persisted `SecurityFinding`, detail loads session+user+harness+audit, status update + audit |
| **Lockdown** (`handleSecurityLockdown`) | **REAL DB cascade** — terminates active sessions + sets harness `risk_state=high` + audit (better than harness revoke, which doesn't cascade). **No live relay propagation.** |
| **Rules** (`handleGetSecurityPolicy`) | **FAKE** — returns a **hardcoded 10-rule list** (PII/secrets/injection), not from DB |
| **Rule update** (`handleUpdateSecurityPolicy`) | **NOT PERSISTED** — comment: *"In production, this would persist to a PolicyPack record. For now, record in audit"*; toggles only write an audit row, so `GET` always returns the same `enabled:true` list |
| Live-path detection | **NOT WIRED** — `relay` imports `security` but `forwardAIToPIA`/`forwardAIHTTP` never call `CheckContext`; so `RecordFinding` is never invoked by real traffic → the findings list stays empty except for seed/manual scans |

## Gaps — grounded

### A. Correctness (the page misleads)
**A1. Rule toggles don't persist.** Hardcoded GET + audit-only PUT → the Rules tab looks editable but changes never stick and never affect detection. *Fix:* persist rules in a `PolicyPack`/DLP-rule record; have `CheckContext` read the effective enabled rule set; GET returns actual state.
**A2. Findings list is empty in practice.** Detection isn't on the live path, so nothing creates findings from real traffic. *Fix:* wire `CheckContext` into the relay pipeline (pre-route) so context is scanned before PIA dispatch and findings are recorded on real violations (ties to Domain 1–2 P0 + Policy A2).

### B. Modeled-but-unwired
**B1. Lockdown is DB-only.** Sessions terminated + risk high in DB, but the relay doesn't enforce state → a "terminated" session's live stream keeps going until the relay checks. *Fix:* propagate lockdown to the relay (kill live exchanges).
**B2. Incident object missing.** Findings have `status` but there's no `Incident` lifecycle (open→triage→contain→resolve→postmortem) despite an `incident` package existing. *Fix:* group findings into incidents with a workflow.

### C. Genuinely missing
**C1. Suppress / accept-risk workflow** — no approver + expiry for tuning false positives.
**C2. Alert routing** (§10C.14) — Slack/email/on-call; today lockdown just logs.
**C3. SIEM/export forwarding** (§32.4) — findings CSV exists, no streaming forwarder.
**C4. Inline response inspection** (§16.5) — only outbound context is scanned; model responses (exfil in output) aren't checked.
**C5. Korean-PII lexicon versioning** (§16.3) — patterns are hardcoded; needs a versioned, org-overridable pack.

## UX improvements (grounded)
1. Rule toggles silently do nothing (A1) — misleads the analyst.
2. No tabs guidance — dashboard/rules/findings/scanner with no suggested order or "start here".
3. Rule builder uses free-text regex (requires expertise) — add a tester with match highlighting + preset library.
4. Findings not deep-linkable to the offending session/exchange (detail modal only).
5. No filter by severity/status/type/time on findings (only the recent 100).
6. Dashboard stat cards (critical/high/medium/open) not clickable to filtered findings.
7. No bulk resolve/suppress.
8. Scanner has no progress/scope (single textarea); no "scan this session" action.
9. Lockdown is one click with only `alert('긴급 잠금 활성화')` — needs 2-step confirm + scope (org vs project) + impact preview.
10. No suppress/accept-risk UI (C1).
11. No favorites/pinned rules; no sub-menu by family (PII / secrets / injection / network).
12. CSV export exists but no date-range scoping.
13. No diff view when editing a rule.
14. No empty-state guidance ("findings appear when detection runs on sessions").
15. No animation/transitions; static tables.

## Sequencing
Phase 1 (stop the lie + make it live): A1 (persist rules), A2 (wire detection onto the relay pipeline) — without A2 the SOC has no real data.
Phase 2 (response): B1 (live lockdown propagation), B2 (incident lifecycle), C1 (suppress/accept-risk), C4 (response inspection).
Phase 3 (enterprise): C2 (alert routing), C3 (SIEM), C5 (PII lexicon versioning).
