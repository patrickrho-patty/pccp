# 06 — 거버넌스 정책 · Policy (`web/src/pages/Policy.tsx`)

> Vertical read: component → `api.ts:91` (listEpochs/createEpoch) → `server.go handleCreateEpoch/handleIssueLease` → `policy.CreatePolicyEpoch/IssueCapabilityLease` → `models/model_registry.go PolicyEpoch/CapabilityLease`, `models/tool_runtime.go PolicyPack`.

## What this page actually is
Admin authoring of **governance policy** — the §13 layered hierarchy (Patty mandatory → profile → org → affiliate → department → project → repo → branch → session) that governs every harness action. Policy is expressed as **epochs** (versioned, immutable-digest, transition-mode) that sessions/leases bind to. This page *should* be where an admin defines model/tool/data/network/SCM/session policy and publishes it as an epoch the harnesses enforce.

## Current vertical (what exists) — and what's fake
| Layer | Reality |
|---|---|
| Component | 3 tabs (active / templates / epochs); a rich **6-domain template catalog** (models, tools, data/DLP, SCM, network, session) with sensible configs; a builder (domain→template→scope); a static policy-hierarchy visualization |
| **Where "active rules" are stored** | **`localStorage` only** — `localStorage.getItem/setItem('pccp_policy_rules')`; code comment: *"Load saved rules from localStorage for now (would be API in production)"*. **Nothing about non-model policy reaches the backend.** |
| `addRule` side-effect | calls `api.createEpoch({allowed_models: rule.config.allowed_models || ['patty-code-standard']})` — so adding **any** rule (even a "network" or "DLP" one) creates a **model allow-list epoch**. Nonsensical. |
| `handleCreateEpoch` | accepts only `{organization_id, allowed_models, transition_mode}` — **only model policy**; no DLP/tool/network/SCM/session dimensions |
| `PolicyEpoch` model | **rich** — `OrgPolicyDigest`, `ProjectOverlayDigest`, `ModelPolicyDigest`, `DLPSecurityDigest`, `ApprovalMatrixDigest`, `RetentionPolicyDigest`, `EpochNumber`, `TransitionMode`, `SupersededBy` — but `CreatePolicyEpoch` fills **only** `AllowedModelsJSON`; all other digests stay empty |
| `CapabilityLease` / `PolicyPack` | modeled, unused by this page |
| Enforcement | none — epochs aren't bound by `OpenSession` (Sessions A1), aren't checked by the relay pipeline (dead), so even the model epoch does nothing today |

## Gaps — grounded

### A. The page is largely fictional (fix the foundation)
**A1. Rules are localStorage-only.** The whole "active policies" tab is client state — reload on another machine and it's gone; nothing enforces it. *Fix:* persist rules server-side as a `PolicyPack`/`PolicyRule` record (model `PolicyPack` exists) keyed by domain+scope; load via API.
**A2. Only model allow-lists are real (and even those unenforced).** *Fix:* extend `handleCreateEpoch`/`CreatePolicyEpoch` to accept all domains (tool perms, DLP, network, SCM, session) and fill the corresponding digest fields already on `PolicyEpoch`; publish one coherent epoch.
**A3. `addRule` creates a model epoch for every domain.** *Fix:* map each domain to its policy dimension; only model rules touch `AllowedModelsJSON`.
**A4. Epochs aren't enforced** — `OpenSession` doesn't bind one (Sessions A1), the relay pipeline is dead (Domain 1–2). The model epoch is inert until those are wired.

### B. Modeled-but-unwired
**B1. Policy hierarchy is a static picture.** No group/affiliate model yet (Users F3); no effective-policy resolver (walk hierarchy → merge → result). *Fix:* a resolver `EffectivePolicy(org, project, repo)` merging layers (lower can only strengthen, never weaken — matches the page's own caption).
**B2. `PolicyPack` unused.** *Fix:* group rules into versioned packs; assign packs to orgs/projects (Project A3/B2).
**B3. Epoch versioning unused** — `EpochNumber`/`SupersededBy` exist; no diff/rollback UI.

### C. Genuinely missing
**C1. Approval workflow for policy changes** (§46.2) — `configmgmt` has a lifecycle state machine, but policy changes publish immediately.
**C2. Mandatory acknowledgement campaign** (§33.6) — epoch change should require user ack (ties to Users B-feature + harness D).
**C3. Policy simulation** (§15.5) — test a proposed epoch against historical sessions.
**C4. Conflict detection** between overlapping epochs; **C5. Exception marketplace** (§33.8).

## UX improvements (grounded)
1. **Rules vanish on reload/other machine** — localStorage (A1); the toggle/delete are client-only.
2. Templates are hardcoded constants (6 domains × N templates) — should be server-side, editable, versioned.
3. Scope picker (org/project/repo/team) is free-text "범위 이름" — should pick a real entity.
4. Creating a rule shows no preview of *who/what is affected*.
5. No diff between epochs (B3).
6. No approval/staging (C1) — "정책 활성화" is instant + irreversible from the UI.
7. Epoch table not filterable/sortable; transition-mode unexplained.
8. allowed_models in epochs is a bare list — not bound to catalog IDs (§10A).
9. No favorites; no sub-menu by domain.
10. No empty-state guidance; no search across policy contents.
11. Hierarchy visual is non-interactive — clicking a layer should show its effective rules.
12. No bulk enable/disable; no export/import packs.
13. No indication that nothing is currently enforced (misleading green "활성" badges).
14. Builder modal has no cancel/reset state cleanly; scope name defaults to "전체 조직" always.
15. Templates tab "+ 활성화" can add duplicates (no dedupe by template_id+scope).

## Sequencing
Phase 1 (stop the lie): A1 (server persistence), A2/A3 (multi-domain epochs), and remove the misleading "활성" enforcement implication until A4 is wired.
Phase 2 (make it real): A4 (bind+enforce via Sessions A1 + relay pipeline), B1 (effective-policy resolver), B2 (packs).
Phase 3 (enterprise): B3 (versioning/diff), C1 (approvals), C2 (acknowledgement), C3 (simulation), C5 (exceptions).
