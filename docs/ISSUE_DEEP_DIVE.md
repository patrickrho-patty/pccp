# PG-UI Issue Deep-Dive — current state of the 8 selected issues

Investigation date: 2026 (concurrent agents have since built the *other* 8
campaign/campaign-family issues: 1449/1454/1439/1444/1450/1451/1453/1437 — see
commit log). This doc maps what already exists in the PCCP repo for the 8
issues we selected (1456/1455/1440/1403/1404/1442/1443/1452), what is reusable,
and what is genuinely new.

## PAT-1456 — managed skill governance
- **Exists:** `internal/skillpolicy/` — pure three-state resolution engine
  (Required/Optional/Blocked; scopes org/team/fleet/user; digest identity;
  fail-closed unknown; narrow-can't-weaken). Committed `efd7984`, 8 tests.
- **Reusable:** `internal/policy/epochs.go` precedence, `internal/models/relay_control.go`
  `RelayControlEvent` signed directive carrier.
- **Needed (remainder):** persisted assignment model; admin API + Skills page UI
  (search/filter/effective-state/drift/impact counts); signed epoch delivery
  over `RelayControlEvent`; audit. Harness enforcement lives in patty-code
  (out of this repo).

## PAT-1455 — managed system-prompt controls
- **Exists:** nothing specific. No `PromptAddition`/`SystemPrompt`/prompt-policy
  model in PCCP.
- **Reusable:** the same policy-epoch + signed delivery + scope resolution
  foundations as 1456; `RelayControlEvent` distribution + ack.
- **Needed (remainder):** a prompt-addition policy-domain model (immutable
  versions + digest + target scope), validation (secret/oversize/unsupported
  interpolation/budget), admin editor+preview UI, dichotomy to the existing
  policy epoch, signed distribution + harness ack. Harness prompt-composition
  builder lives in patty-code.

## PAT-1440 — evidence-backed leaderboard
- **Exists:** `internal/workintel/service.go` prototype `GenerateScorecard`.
  **Confirmed defects (spec says don't ship):** it directly rewards
  `ChangesCreated*10`, `LinesAdded*0.5`, `AIInferences*5`, `SecurityFindings*20`
  (raw counts); user-scope is wrong (lines/security queries are org-wide, not
  user-scoped).
- **Reusable:** usage/engineering-metric foundations, audit/provenance events,
  change sets, reviews UI.
- **Needed (remainder):** fresh four-property engine (accepted delivery,
  first-pass quality, security/governance adherence, delivery efficiency) with
  proper cohorting/minimum-evidence, rubric versioning + frozen-at-period,
  weights-with-bounds, anti-gaming (coalesce split, de-dupe objectives),
  correction/dispute, insufficient-evidence state; fix user scoping; UI.

## PAT-1403 — HWP/HWPX internal tools
- **Exists:** nothing in PCCP. No hwp/hwpx parsing in this repo.
- **Reusable precondition:** the engine lives in **lawyerkit** (`projects/one`
  `@rho/editor`, `parseHwpxFull`/`serializeHwpxFull`) — a **separate repo +
  extraction step** not in PCCP.
- **Needed (remainder):** extract `packages/hwp-engine` (prerequisite, lawyerkit
  repo) → vendor into patty-code (`vendor/hwp-engine` + `ENGINE.manifest.json`
  + bump script) → build `bin/hwp-engine` → register `read_hwp`/`read_hwpx`
  internal tools. This spans two external repos.

## PAT-1404 — Patty Reference retrieval
- **Exists:** **no `internal/productdocs` in PCCP** (spec's "reusable" list was
  about patty-code, whose `docs/embed.go` + `internal/productdocs` are there).
  PCCP has `internal/mcp/service.go`, `internal/tools/service.go` (capability
  leases/allowlists/integrity).
- **Reusable in PCCP:** MCP policy/governance, tool capability enforcement,
  sandbox air-gapped modes.
- **Needed (remainder):** corpus/package schema (signing/digest/archive), source
  registry, retrieval API/MCP contract, version resolver, hybrid BM25 ranking,
  4 deployment paths, admin package-management UI. Very large.

## PAT-1442 — company-wide SSO Keycloak→Authentik
- **Exists:** `internal/sso/` (service.go, flows.go, oidc_verify_test.go). It
  implements initial OIDC/SAML service behavior.
- **Reusable:** OIDC/SAML verify + flows foundations, account worker, device-
  auth seed.
- **Needed (remainder):** this is an **org-wide migration** — discovery of the
  deployed Keycloak estate (not fully in-repo), Authentik deployments per trust
  domain, blueprints, identity import + reconciliation, compatibility bridge,
  staged cutover waves, retirement. Mostly infra/ops + a compatibility adapter.
  Largest-scope item of the 8.

## PAT-1443 — GPU queue / inference QoS ops analytics
- **Exists:** `internal/scheduler/` is extensive — `queue/queue.go` `Request`
  has `ArrivedAt`/`Deadline`/TTL/SLO; `dispatch.go` worker load; `observability.go`
  admin views (fleet/queue/cache/performance/routing/scaling). `internal/telemetry`
  has counters/gauges/histograms but its percentile is insertion-ordered (spec:
  don't reuse as authoritative).
- **Reusable:** scheduler lifecycle timestamps, queue model, observability views,
  telemetry.
- **Needed (remainder):** authoritative request-timeline recording (ingress→
  first token→completion with outcomes), **correct percentile/durable aggregation**
  (replace insertion-ordered), wait-time estimator (EWMA + calibration), SRE-only
  ops page + KPIs, anonymized drill-down, retention/cardinality controls.

## PAT-1452 — hardened dev environments / sandbox lifecycles
- **Exists:** `internal/sandbox/` with `runtime.go`, `lifecycle.go`, adapters,
  forensic snapshots; sandbox policy (remote/local), evidence/audit.
- **Reusable:** sandbox runtime contract + Docker adapter, lifecycle, policy,
  evidence.
- **Needed (remainder):** ephemeral/persistent/pinned lifecycle engine +
  runner-pool registry + templates/repo-mapping + placement/drift + admin UI +
  hardening-baseline capability attestation.

## Cross-cutting
- Signed directive + ack carrier: `RelayControlEvent` / `RelayControlAck` (shared
  by 1455/1456).
- Policy epoch + scope precedence: `internal/policy/epochs.go` (lower-layer-wins,
  restrictions-never-removed).
- These 7 remaining issues are each multi-session feature epics, not single
  tickets. Harness-side enforcement for 1456/1455 and the 1403 engine extraction
  live outside the PCCP repo.