# 04 — 프로젝트 · Projects (`web/src/pages/Projects.tsx`)

> Vertical read: component → `api.ts:64` → `server.go` project handlers (708/732/1711/1772) → `identity.CreateProject` (210) → `models/project.go Project`.

## What this page actually is
Admin management of **Projects** — the organizational unit that groups repositories, sessions, members, and an allowed-model policy under one governance scope (§12). A project is the natural boundary for entitlement (who may work on what), policy (which models/repos), and analytics/chargeback rollup. Org-scoped via the operator JWT.

## Current vertical (what exists)
| Layer | Reality |
|---|---|
| Component | card grid, create/edit form (name, name_ko, slug, allowed_models comma-string, description), expandable card; stats (repos/active-sessions/all-sessions/members); archive action |
| `Project` model | `AllowedModelClasses` (JSON array), `PolicyPackID`, `ProjectCode`, `GroupAffiliate` (Korean attrs), `Status`{active,archived} |
| `handleCreateProject` (708) | accepts `{organization_id,name,name_ko,slug,allowed_models}` — **`description` from the form is dropped**; calls `identity.CreateProject` |
| `identity.CreateProject` (210) | sets name/name_ko/slug/status/allowedModels; no description, no PolicyPackID/ProjectCode/GroupAffiliate |
| `handleUpdateProject` (1711) | accepts `{name,name_ko,description,status}` — **`allowed_models`/`slug` NOT updatable** (create-only) |
| `handleDeleteProject` (1772) | soft archive (`status='archived'`) — correct; **no cascade** (repos/sessions remain attached) |
| `handleGetProject` (732) | exists, **unused** (no detail page) |
| Membership | **derived** — `getProjectMembers` counts users who have a session in the project; no real membership/role model |

## Gaps — grounded

### A. Modeled-but-unwired
**A1. `description` dropped on create.** Form sends it; handler/service ignore it. *Fix:* accept `description` in `handleCreateProject` + `identity.CreateProject`.
**A2. `AllowedModelClasses` create-only + free-text.** Update can't change it; the form is a comma-string requiring knowledge of model class IDs. *Fix:* multi-select from the active catalog epoch (§10A); make it updatable on edit; store canonical catalog-model IDs.
**A3. Korean attrs unused.** `ProjectCode`, `GroupAffiliate`, `PolicyPackID` exist but aren't set/editable. *Fix:* expose in the form (project code for Korean enterprise tracking, group/affiliate picker once hierarchy exists, policy-pack assignment).

### B. Genuinely missing
**B1. No real project membership.** Members are inferred from sessions — there's no "who is on this project" with a role. *Fix:* a `ProjectMember{project_id, user_id, role}` table + assignment UI; drives entitlement (§13) and analytics.
**B2. No project policy pack binding.** `PolicyPackID` is a field but projects don't carry an effective policy; model allow-list is the only governance. *Fix:* bind a policy pack (epoch) per project; surface effective policy.
**B3. No detail page** (`handleGetProject` exists). *Fix:* `/projects/:id` with repos, members+roles, sessions, policy, usage/chargeback, audit.
**B4. Archive has no cascade/restore.** Archiving leaves repos/sessions attached and active; no restore. *Fix:* archive should freeze new sessions on the project (§33.13-style) and offer restore.
**B5. No server-side query** — `handleListProjects` returns all, client filters.
**B6. No chargeback/budget** (§29.12) — projects are the natural cost center.
**B7. No AI Change-Control queue per project** (§33.4) — high-risk changes route here.

## UX improvements (grounded)
1. `description` silently dropped on create (A1).
2. `allowed_models` free-text comma string → catalog multi-select; not editable after create (A2).
3. Slug manual entry → auto-generate from name (editable, locked after create — already disabled on edit, good).
4. Members stat is derived from sessions, not real membership → misleading; show real roster (B1).
5. Stats (repos/sessions/members) not deep-linkable to filtered lists (active-sessions links go to generic `/sessions`).
6. No project detail page (B3) — card "상세" only shows id + recent sessions.
7. No bulk archive; no restore.
8. No filter by status/group-affiliate.
9. "+ 저장소 연결" is just a link to /repositories, not an inline attach flow.
10. No column/card sort; no favorites.
11. No empty-state guidance; no skeleton.
12. No left sub-menu (Active / Archived / Templates).
13. No responsive reflow beyond 2-col grid.
14. Archive confirm generic; no impact preview ("3 active sessions will be frozen").
15. allowed_models badges not clickable to model detail.

## Sequencing
Phase 1 (correctness): A1 (description), A2 (catalog multi-select + updatable), B5 (server query).
Phase 2 (structure): B1 (real membership), B3 (detail page), A3 (Korean attrs).
Phase 3 (governance/commercial): B2 (policy pack), B4 (archive cascade), B6 (chargeback), B7 (change-control queue).
