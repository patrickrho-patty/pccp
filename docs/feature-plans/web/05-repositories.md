# 05 — 저장소 · Repositories (`web/src/pages/Repositories.tsx`)

> Vertical read: component → `api.ts:74` → `server.go` repo handlers (register/update/delete/baseline) → `identity.RegisterRepository` (233) → `models/project.go Repository/RepoBaseline`.

## What this page actually is
Admin registry of **Git repositories** attached to projects — the SCM substrate that sessions branch from and that provenance annotates (§18). A repo carries a project binding, default branch, sensitivity classification, and (per §18.3) immutable task baselines. It is *not* today a live SCM integration — it's metadata.

## Current vertical (what exists)
| Layer | Reality |
|---|---|
| Component | table, create/edit form (name, slug, project, scm_provider, clone_url, default_branch), expandable row; branch-protection via `prompt()`; edit/delete actions |
| `Repository` model | `ProjectID`, `CloneURL`, `SCMType`, `SCMProvider`, `DefaultBranch`, **`Sensitivity`**{public,internal,confidential,restricted}, `Status`; sibling `RepoBaseline` exists (§18.3) |
| `api.ts:74` | **only `registerRepository` + `updateRepository`** — no `createRepository`, no `deleteRepository` |
| Component calls | `api.createRepository(form)` and `api.deleteRepository(id)` — **both undefined → runtime TypeError → Create and Delete are broken** |
| `handleRegisterRepository` | accepts `{organization_id,project_id,name,full_name,default_branch,sensitivity}` — **drops `clone_url`, `scm_provider`, `slug`** the form sends |
| `identity.RegisterRepository` (233) | sets name/fullName/SCMType="git"/defaultBranch/sensitivity/status; ignores CloneURL/SCMProvider |
| `handleDeleteRepository` (1796) | exists (backend OK) — unreachable from the UI |
| Branch protection | `POST /api/scm/branch-protection` with a `prompt()`-entered level 1–5; level stored, no real enforcement |

## Gaps — grounded

### A. Broken / correctness (fix first)
**A1. Create & Delete are broken.** `api.createRepository`/`api.deleteRepository` don't exist in `api.ts` → the buttons throw. *Fix:* add `createRepository`→`POST /api/repositories` and `deleteRepository`→`DELETE /api/repositories/{id}` (or call the existing `registerRepository`).
**A2. `clone_url`/`scm_provider` dropped on create.** Handler/service ignore them. *Fix:* accept + persist in `handleRegisterRepository`/`RegisterRepository` (model already has the columns).
**A3. `sensitivity` not settable from the form** (defaults `internal`). *Fix:* add a sensitivity `<select>` (drives §33.5 heatmap + §27 access).
**A4. Branch protection via `prompt()`** — awful UX, free-text level. *Fix:* a real modal with level descriptions + per-branch rules.

### B. Modeled-but-unwired
**B1. `RepoBaseline` unused on the page.** `identity.CreateBaseline` exists (§18.3 immutable task baseline). *Fix:* show/record baselines per repo/branch; anchor sessions to a baseline.
**B2. `Sensitivity` drives nothing.** Should feed the repo sensitivity heatmap (§33.5) and access gating.

### C. Genuinely missing
**C1. No real SCM integration** *(your explicit complaint)* — clone_url is inert text; no clone, no file browser, no webhooks, no GitHub/GitLab API. *Fix:* a `gitscm` connector that clones/reads trees + webhook ingestion; surface a file browser (ties to CodeExplorer).
**C2. No file/branch browser** — can't browse files (your complaint).
**C3. No repo detail page** (`handleGetRepository` exists) — `/repositories/:id` with branches, baselines, sensitivity heatmap, sessions, findings.
**C4. No server-side list query.**
**C5. No commit/merge provenance binding** (§18.6) — repos aren't linked to commits/ChangeSets from the UI.

## UX improvements (grounded)
1. **Create/Delete broken** (A1) — page can't add or remove repos today.
2. `clone_url`/`scm_provider` silently dropped (A2).
3. No sensitivity selector (A3) — heatmap/access can't work.
4. Branch-protection `prompt()` (A4) → real modal with rule detail + enforcement status.
5. No file browser (C2) — the "🔬 프로바이던스 탐색" link is the only entry, and it's empty until provenance flows.
6. Project cell links to generic `/projects`, not the specific project.
7. SCM provider is a hardcoded `<select>` (github/gitlab/...) — should be configured connectors (C1).
8. Clone URL plain text — no validation, no auth-credential picker.
9. No sync/pull status; no last-commit time.
10. No filter by project/sensitivity/status (beyond SCM).
11. No bulk unregister; no favorites.
12. No empty-state guidance ("첫 저장소 연결" → SCM connector).
13. No webhook/secret config UI.
14. ID/clone-url not copyable.
15. No left sub-menu (Repos / Baselines / Branch rules / Webhooks).

## Sequencing
Phase 1 (make it work): A1 (fix create/delete), A2/A3 (persist clone_url/provider/sensitivity), A4 (branch-protection modal).
Phase 2 (real SCM): C1 (connector: clone/read/webhook), C2 (file browser), C3 (detail page).
Phase 3 (governance): B1 (baselines), B2 (sensitivity→heatmap/access), C4 (server query), C5 (commit provenance).
