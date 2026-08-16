# S2–S12 Execution Status (pccp-fabric)

Branch: forge/pccp-fabric. The layer carries no separate product brand — it is internal PCCP code (the governed-inference fabric: gateway/admission/routing/KV/SLO/autoscale/resilience/observability). 'Dynamo' was only the upstream inspiration project from the original plan title. All S2–S12 sub-projects from
docs/superpowers/specs/2026-08-15-s1-fleet-registry-design.md are implemented,
tested, and committed. Full suite: go build + go vet + go test ./... green
(42 packages).

## Completed commits (this session)
- S2 core: token-debited DRR queue, overload gate, model rewriting, gateway,
  dispatch (920876a..)
- S2 e2e: serving handler, relay scheduler hop, DARI forwarder, card v2
- S2 streaming + signed traffic classes
- S2 per-tenant model access
- S3: KV index, cost router, gang readiness, KV journal, LoRA affinity,
  model pools, signed routing receipts
- S4: Bayesian predictor (TTFT/TPOT pair, evidence-scaled variance)
- S5: SLO resolver, MTP capacity, router SLO gate
- S6: tiered KV, P/D roles, topology cost
- S7: dual-loop autoscaler, engine lifecycle, warm pool, LoRA lifecycle,
  cost optimizer
- S8: resilience (prober, migration, cancellation, shadow failover)
- S9: batch gateway
- S10: observability + routing explainability
- S11: digital twin + autotune
- S12: K8s adapter (CRDs, GAIE v1.5, pod->card, HPA/KEDA)
- Conformance: s2_s12_conformance_test.go
- NewScheduler composes the full stack; binary loops wired
  (autoscale/health); DARI AI_OPEN ingress + KV journal e2e tests

## Remaining plans (next)
1. docs/feature-plans/web/* (26 pages + 00 cross-cutting) — the web
   feature plans. Some pages implemented on main (01, 02, 10-12, 24-26 per
   main commits). Remaining pages need implementation in web/src/pages,
   web/src/api.ts, internal/api handlers.
2. docs/feature-plans/harness/* (A-F) — harness worktree work. F
   (benchmark) not started.
3. Push to origin as we go (forge/pccp-fabric).

## Blockers
- blockers.md has entries: audit flake (resolved), sandbox Docker flake
  (environmental).


## Web feature plans (docs/feature-plans/web/00-26) — ALL DONE
All 27 web plans (00 cross-cutting + 26 pages) are implemented on
forge/pccp-fabric. Backend verticals + frontends per page:
- 00 cross-cutting: shared infra (favorites/server-table/StatCard/
  EntitySelect/⌘K/theme/motion/responsive/search/detail routes)
- 01 users, 02 sessions, 03 harnesses, 04 projects, 05 repositories,
  06 policy, 07 security, 08 compliance, 09 fleet, 10 sre, 11 scc,
  12 subscriber-management, 13 communications, 14 tools, 15 sandboxes,
  16 analytics, 17 audit, 18 model-infra, 19 code-explorer,
  20 provenance, 21 live-view, 22 enterprise-features, 23 dashboard,
  24 account-portal, 25 login (TOTP MFA + throttle), 26 bootstrap.
Blockers logged in blockers.md #3-#6 (operator SSO, PSP, harness-side
telemetry, PIA-side load verification — all cross-repo/dependency).

## Paper plans — ALL DONE (see commits 644e466..adfd4f4)
Visual-evidence 24/24, rhetoric-audit 11/11, Korean-edition QA,
positioning superseded note. Manuscripts rebuild clean (EN/KO, zero
warnings, fonts embedded, archive verified from a fresh unpack).
