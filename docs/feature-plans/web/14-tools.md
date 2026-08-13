# 14 — 도구 관리 · Tools (`web/src/pages/Tools.tsx`)

> Vertical read: component → `api.ts listTools/seedTools` + `fetch /api/tools/{,id}` → `services.go` tools routes (192–206) → `tools.RegisterTool/SeedDefaultTools/ListTools` → `models/tool_runtime.go Tool`. DB verified.

## What this page actually is
The **Tool Registry** (§17.1) — the org's catalog of tools the harness may use, each classified by category (read/write/execute/network), danger level, and whether it requires approval. It's the admin side of tool/MCP governance; approval decisions and harness enforcement happen elsewhere.

## Current vertical (what exists)
| Layer | Reality |
|---|---|
| Component | list (FilterBar), register/edit form (name, name_ko, category, tool_class, danger_level, requires_approval), seed button, toggle approval, delete |
| Routes (`services.go:192`) | `GET/POST /tools`, `POST /tools/seed-defaults`, **`GET /tools/approvals` + `POST /tools/approvals/{id}/decide`** (approval workflow exists backend-side) |
| `tools.RegisterTool` | **dedupes by `organization_id + name`** (returns existing if present) — idempotent |
| `SeedDefaultTools` | seeds 10 defaults via `RegisterTool` (idempotent); DB verified: **10 distinct tools per org, no intra-org duplicates** |
| `wrapToolsList` | **org-scoped** via `getOrgID` |
| `Tool` model | `Category`, `ToolClass`, `Signature` (integrity digest, unused), `AllowedByDefault`, `RequiresApproval`, `DangerLevel` |

➡️ **Correction to the earlier gap doc:** the "click 기본 도구 등록 → duplicates" bug you reported is **resolved in current code** (`RegisterTool` dedupes; DB clean). What remains is the lack of legacy-dedupe cleanup and no UI guard/feedback.

## Gaps — grounded
**A. Tools aren't enforced at request time.** The registry is admin-only metadata; nothing on the harness/relay path checks "is this tool registered/approved for this org?" before allowing a tool call (§17.1). *Fix:* the relay pipeline (or harness) resolves tool calls against the registry + approval state; unapproved → block.
**B. Approval workflow exists but isn't surfaced.** `/tools/approvals` + `/decide` endpoints exist; the page only toggles `requires_approval` — no pending-approvals queue or decision UI. *Fix:* an Approvals tab.
**C. MCP governance lives elsewhere** (`/api/mcp/*`, `mcp` package) and isn't on this page — tools + MCP are split. *Fix:* unify or cross-link (§17.2).
**D. Classification fields need expertise.** `tool_class`/`category`/`danger_level` are free-text/hardcoded selects with no guidance — admins don't know what to enter *(your "what is this page for?" concern)*. *Fix:* presets + descriptions; a "register custom tool" wizard with examples.
**E. `Signature` (tool integrity digest) unused** — no verification that a registered tool matches its digest at runtime.

## UX improvements (grounded)
1. No indication the page's purpose is *governance* (registry → enforcement) — add context/help.
2. Seed button gives no feedback ("already seeded"/"10 added"); no cleanup of legacy dupes.
3. `tool_class`/`category` free-text — presets + a custom-tool wizard.
4. Approval queue missing (B) — `requires_approval` toggles but nothing to action pending requests.
5. MCP not represented here (C).
6. No tool detail page; name not deep-linkable.
7. No filter by category/danger/approval (beyond search).
8. No bulk approve/enable/disable.
9. Danger level label without explanation (what's "critical" block?).
10. No favorites; no sub-menu (Tools / MCP / Commands / Networks / Secrets per §17).
11. No empty-state guidance ("seed defaults or register a custom tool").
12. No export; no versioning of tool definitions.
13. Toggle approval is a single click with no audit/confirm.
14. No responsive layout.

## Intended-features coverage (vs WEB_FEATURE_GAPS §12 — 10 features)
1. Tool/MCP governance enforced at request time → **A** ✅
2. MCP server registry + approval workflow → **B/C** ✅ (approval queue surfaced)
3. Command-authorization policy editor → folded into §17 sequencing P3; **add** a command-policy editor tab.
4. Network-broker allow/deny rules → folded into §17 sequencing P3; **add** network-rule management.
5. Secret-broker scope management → folded into §17 sequencing P3; **add** secret-scope management.
6. Tool risk classification + approval SLA → **D** partial (classification); **add** approval SLA.
7. Per-project tool allowlist → **add** (not present); project-scoped allowlist.
8. Tool-call audit (which session called which tool) → **add**; tool-call log wired once tools enforced.
9. Custom tool registration → **D** (custom-tool wizard) ✅
10. Tool capability/compatibility matrix vs models (§10B.4) → **add**; capability matrix view.

## Sequencing
Phase 1 (clarity + correctness): purpose/help text, custom-tool wizard, approval queue (B), MCP cross-link (C), seed feedback + legacy dedupe.
Phase 2 (governance): A (enforce registry on the request path), E (signature verification).
Phase 3 (enterprise): command/network/secret brokers surfaced under one §17 menu, bulk ops, versioning.
