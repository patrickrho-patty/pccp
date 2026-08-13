# 01 — 사용자 · Users (`web/src/pages/Users.tsx`)

> Grounded in the actual vertical: component → `api.ts` → `server.go` handlers → `identity` service → `models/identity.go`. Every claim cites code.

## What this page actually is
It manages the **`users` table = the managed developer / harness-user population** (the governed subjects — 김개발, 이테스트, 박보안, 최리드). Evidence: `auth_method`+`external_id` (SSO subject), `employee_id`, `business_unit_id`, `contractor_info`. **These users do not log into the console** — only `admin_credentials` does (`auth.Login` checks `admin_credentials`; `handleLogin`). The page is operated by an **admin** whose JWT `org_id` scopes every list via `getOrgID(r)` (`server.go:420`).

➡️ **It is NOT about PCCP console operators.** Operator authorization (who can use which console area) belongs to `admin_credentials` and is a separate plan. `Role`/`UserRole` point at `users` with `scope∈{project,org,global}` — that reads as **scoped developer entitlement** (what a developer may touch via their harness), not console RBAC. This page owns developer identity + entitlement, not console authz.

## Current vertical (what exists)
| Layer | Reality |
|---|---|
| Component | list (client-side FilterBar filter + slice pagination), create/edit form, bulk suspend/offboard, expandable row (sessions + harnesses-derived + quick info) |
| API (`api.ts:42`) | `GET/POST /api/users`, `GET/PUT/DELETE /api/users/{id}` |
| `handleListUsers` (577) | org-scoped by operator JWT; returns **all** rows — no server pagination/filter/sort |
| `handleCreateUser` (588) | accepts **only** `{organization_id,email,name,name_ko,auth_method,title}` — **`business_unit_id` from the form is silently dropped**; dedups email+org; calls `identity.CreateUser` |
| `handleGetUser` (626) | returns the user — **endpoint exists, UI never uses it** |
| `handleUpdateUser` (1711) | accepts only `{name,name_ko,email,title,status,auth_method,locale,timezone}` — ignores `business_unit_id, contractor_info, employee_id, access_labels, mfa`; **no audit** |
| `handleDeleteUser` (1744) | sets `status='offboarded'` — **no session/harness revocation, no audit** |
| `identity.CreateUser` (55) | sets base fields only; **does not call `recordAudit`** (exists at 312) |
| Related models that **exist but are unwired** | `BusinessUnit` (hierarchy: type affiliate/business_unit/department/team, level, parent_unit_id); `Organization` (seats `max_user_seats`/`max_harness_seats`, `parent_org_id`, `group_company`); `Harness.AllowedUsers` + `Device.UserID` (real harness↔user binding); `EnrollmentCode` (+ `identity.GenerateEnrollmentCode` at 328); `Role`/`UserRole` (empty) |

## Gaps — grounded, developer-scoped

### A. Modeled-but-unwired (wire the existing schema — cheap, high-value)
**A1. Department is dead because handlers ignore it.** `BusinessUnit` (full hierarchy) exists in the model, but `handleCreateUser`/`handleUpdateUser` don't accept `business_unit_id`, and the form uses a hardcoded 8-option `<select>`. *Fix:* accept `business_unit_id` in both handlers; add `GET/POST/PUT/DELETE /api/business-units` (CRUD over `BusinessUnit`); replace the form select with a picker fed by the org's business-unit tree. *(This is the root cause of your original "no way to add/edit departments" complaint.)*
**A2. Harness↔developer binding exists but unused.** `Harness.AllowedUsers` (JSON user-id array) and `Device.UserID` are real bindings; the page instead derives harnesses via sessions (`getUserHarnesses`), so it misses harnesses that have no session yet. *Fix:* list a developer's harnesses from `AllowedUsers`/`Device.UserID` directly; let the admin grant/revoke a harness to a developer here.
**A3. Enrollment code is modeled, not surfaced.** `EnrollmentCode` + `identity.GenerateEnrollmentCode(orgID,userID,validity)` exist. *Fix:* "초대 코드 발급" action per developer (and bulk), showing the one-time code + expiry + used-by harness.
**A4. Seats are modeled, not enforced.** `Organization.MaxUserSeats/MaxHarnessSeats` exist. *Fix:* show seat usage (users/max, harnesses/max) on the page header; block create/enroll at the limit with a clear message; surface over-seat state for billing.
**A5. Contractor is a text blob.** `contractor_info` is `text`. *Fix:* promote to a structured `ContractorProfile` (sponsor_user_id, company, contract_start/end, allowed_repo_ids, allowed_model_classes, network_zone) with a dedicated form section + auto-disable job at `contract_end`.

### B. Genuinely missing
**B1. No audit on any user mutation.** `recordAudit` exists but user handlers don't call it; status changes capture no reason. *Fix:* emit `AuditEvent` on create/update/delete/suspend/offboard; require a reason modal for destructive ops; add `GET /api/users/{id}/audit`.
**B2. Offboarding is a status flip.** `handleDeleteUser` just flips status — no cascade. *Fix:* an `OffboardingCase` workflow that closes the user's sessions, revokes their harnesses (calls fleet), expires comms rooms, packages evidence, and confirms access removal (= 0 sessions/harnesses).
**B3. No server-side list query.** *Fix:* `?page=&size=&search=&business_unit=&status=&role=` with total count; push filter/sort server-side.
**B4. No detail page** despite `GET /api/users/{id}`. *Fix:* `/users/:id` with tabs (Overview / Entitlement / Sessions / Harnesses / Usage / Audit / Contractor); make every name cell a `<Link>`.
**B5. Developer entitlement unwired.** `Role`/`UserRole` (scope project/org/global) are empty. *Fix:* seed developer entitlement roles (e.g. project-scoped developer, repo-reader), add assignment UI + `authz` evaluation that the Relay pipeline consumes (what this developer may do via harness — distinct from console RBAC). Decision is forced by the schema: `UserRole.scope` is clearly scoped entitlement.
**B6. Per-developer usage/cost** — depends on wiring `RecordUsage` (MISSING_ITEMS P0); add `GET /api/users/{id}/usage` + a usage tab.
**B7. SCIM/CSV provisioning** — `external_id` supports idempotent upsert; `sso.HandleSCIMRequest` exists un-routed. *Fix:* route `/scim/v2/*`, add CSV import wizard (dry-run → apply), sync-status indicator.
**B8. SSO connection status not surfaced** — `auth_method`/`external_id` are stored but invisible. *Fix:* show "OIDC 연결됨 / 미연결" + last SSO login; link to org SSO config.

## UX improvements (grounded in the code above)
1. Hardcoded department `<select>` → BusinessUnit picker (A1).
2. Name cell not a `<Link>` → detail page (B4).
3. `handleDeleteUser`/`handleDeleteUser` ambiguity — "퇴사" and "삭제" both flip status; split into suspend / offboard (B2) / purge with distinct confirms; purge requires elevated operator role.
4. Suspend/offboard need a reason modal (B1).
5. `handleListUsers` returns all → server pagination/filter/sort (B3).
6. Expand row loads all sessions+harnesses client-side → lazy `GET /api/users/{id}/activity`.
7. Most fields not editable (contractor/employee/access_labels) → extend `handleUpdateUser` (A5, B8).
8. Bulk actions limited to suspend/offboard → add bulk assign BusinessUnit/entitlement, export-selected.
9. No CSV import/export (B7).
10. Harnesses derived via sessions → use real binding (A2).
11. FilterBar lacks department/entitlement filters (after A1/B5).
12. No column sort/toggle/saved views.
13. No favorites/pinning.
14. No skeleton/transition on expand; no empty-state ("첫 개발자 초대" → A3 enrollment code).
15. No left sub-menu (Users / Departments / Contractors / Enrollment).
16. No avatar/initials + contractor/entitlement visual cue.
17. No stale-account/anomaly indicator (`last_login_at` exists, unused).
18. No responsive (table → cards) layout.

## Sequencing
Phase 1 (root-cause fixes): A1 (department wiring), B3 (server query), B4 (detail page), B1 (audit+reason) — fixes your original complaints and unblocks cross-linking.
Phase 2 (use the existing model): A2/A3/A4/A5 (harness binding, enrollment codes, seats, contractor).
Phase 3 (entitlement + lifecycle): B5 (entitlement), B2 (offboarding), B7 (SCIM), B6 (usage).
