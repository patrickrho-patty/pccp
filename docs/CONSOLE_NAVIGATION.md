# Console Navigation Route Tree (PAT-1518)

Enterprise/Government console navigation is organized around **administrator
jobs**, not implementation modules. Every route maps to exactly one primary job
and one first-level navigation group. No two first-level items share an
ambiguous purpose; `/live` is exposed in navigation; all deep links and
exact-record global-search results are preserved.

## Navigation groups (first level)

| Group | Administrator job | Destinations |
|---|---|---|
| 개요 · Overview | Where do I stand? | `/` Dashboard |
| 조직 · Organization | People, access, and estate | `/users`, `/harnesses`, `/projects`, `/repositories` |
| AI 세션 · AI Sessions | Live/session/model operations | `/live` (Live Wall), `/sessions`, `/fleet` |
| 모델 · Models | Model lifecycle | `/models`, `/catalog`, `/endpoints` |
| 거버넌스 · Governance | Policy, security, compliance, approvals | `/policy`, `/security`, `/compliance`, `/tools` |
| 운영 · Operations | Evidence, analytics, comms, environments | `/analytics`, `/communications`, `/sandboxes` |
| 퍼블릭 클라우드 · Public Cloud | Public-facing surfaces | `/portal`, `/sre` |
| 프로바이던스 · Provenance | Immutable evidence | `/explorer`, `/provenance` |
| 감사 · Audit | Auditor ledger | `/audit` |

## Route → job mapping (every console route)

| Route | Primary admin job | Nav group |
|---|---|---|
| `/` | Dashboard | 개요 |
| `/users`, `/users/:id` | Identity administration | 조직 |
| `/harnesses`, `/harnesses/:id` | Harness estate | 조직 |
| `/projects`, `/projects/:id` | Project governance | 조직 |
| `/repositories`, `/repositories/:id` | Repository/SCM | 조직 |
| `/live` | Live operations | AI 세션 |
| `/sessions`, `/sessions/:id`, `/sessions/:id/provenance` | Session investigation | AI 세션 |
| `/fleet` | Fleet operations + approvals | AI 세션 |
| `/models`, `/catalog`, `/endpoints` | Model lifecycle | 모델 |
| `/policy` | Policy decisions | 거버넌스 |
| `/security` | Security findings queue | 거버넌스 |
| `/compliance` | Compliance evidence/workspaces | 거버넌스 |
| `/tools` | Tool governance + approval queue | 거버넌스 |
| `/analytics` | Usage/cost investigation | 운영 |
| `/communications` | Communications | 운영 |
| `/sandboxes`, `/sandboxes/:id` | Sandbox environments | 운영 |
| `/portal` | Public cloud accounts | 퍼블릭 클라우드 |
| `/sre` | Platform reliability (SRE) | 퍼블릭 클라우드 |
| `/explorer` | Code/provenance exploration | 프로바이던스 |
| `/provenance` | Immutable evidence | 프로바이던스 |
| `/audit` | Auditor ledger | 감사 |
| `/bootstrap` | First-run setup (pre-auth) | — |

## Actionable queue indicators (counts)

Badges appear only for queues with an exact-scoped destination, resolved
through the canonical dashboard metric contract (PAT-1487/1488). Clicking a
badge opens that exact filtered queue — implemented in `web/src/navQueues.ts`:

| Nav item | Metric | Opens |
|---|---|---|
| 보안 (/security) | `open_critical_findings` | `/security?tab=findings&severity=critical,high&status=unresolved` |
| 컴플라이언스 (/compliance) | `open_remediations` | `/compliance?tab=remediation&status=unresolved` |
| 도구 (/tools) | `pending_approvals` | `/tools?tab=approvals` |
| 하네스 (/harnesses) | `quarantined_harnesses` | `/fleet` |

Count 0 renders no badge; unknown metrics render 0; the badge carries an
`aria-label` so color is never the only status signal.

## Guarantees

- Direct URLs, browser history, and deep links resolve (routes unchanged).
- RBAC-aware visibility is preserved (nav groups are static; routes enforce
  permissions server-side).
- The dashboard is not a second full navigation menu — it prioritizes the
  action center and operational conditions.
