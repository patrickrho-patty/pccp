<p align="center">
  <img src="docs/assets/branding/logo.svg" alt="Patty Code Control Plane" width="620"/>
</p>

<p align="center">
  <a href="./README.md">한국어</a>
  &nbsp;·&nbsp;
  <strong>English</strong>
</p>

<h3 align="center">Make every act of AI-assisted development visible, governed, and provable.</h3>

<p align="center">
  PCCP (Patty Code Control Plane) is the shared operating kernel for the entire Patty Code product line.<br/>
  Public Cloud, Enterprise, and Government/Sovereign deployments all run on the same kernel —<br/>
  only policy and deployment topology change.
</p>

## What PCCP Does

PCCP is more than an LLM gateway. Who is using Patty Code, which models they may use, where and
how inference runs, and what evidence must be retained — every decision a request passes through
before touching AI infrastructure is made in one place.

- **One kernel, three profiles.** Patty Public Cloud · Enterprise · Government/Sovereign share
  the same identity model, DARI contracts, and scheduler primitives. No edition-specific code forks.
- **Model discovery is server-authoritative.** The Harness carries no model list and no provider
  URL configuration. Behind authentication it receives only a model catalog filtered by
  entitlement and policy.
- **DARI is the only Harness service protocol.** There is no OpenAI/Anthropic-compatible path or
  REST fallback on the official route. Plain HTTP exists for admin APIs only.
- **The control plane is not the token data plane.** Relays and the scheduler handle the hot path
  from signed state; events flow asynchronously into metering, security, and audit systems.

## Architecture

```text
 developer machine                       PCCP                          inference infra
                          ┌────────────────────────────────┐
                          │  Control Plane          :8080  │
                          │  identity · catalog · policy   │
                          └───────────────┬────────────────┘
                                     signed hot state
                                          ▼
┌──────────────┐   DARI   ┌─────────┐        ┌───────────┐    DARI    ┌─────┐
│ Patty Code   │ ───────► │  Relay  │ ─────► │ Scheduler │ ─────────► │ PIA │ ──► vLLM · SGLang
│   Harness    │          │  :8090  │        │   :8455   │            │:9090│       GPU
└──────────────┘          └─────────┘        └───────────┘            └─────┘
```

The relay accepts harness traffic and makes auth/policy verdicts, the scheduler picks a worker
based on KV cache location and load, and PIA verifies the lease before handing the request to a
local serving engine (vLLM · SGLang). Every hop speaks DARI (CBOR + COSE-Sign1 over QUIC/TCP).

## Components

| Binary | Default port | Role |
|---|---|---|
| `pccp-server` | `:8080` | Control plane. REST API + React web console. Org/user/harness/model/policy management |
| `pccp-relay` | `:8090` / `:8444` | Data plane entry point. DARI auth, lease validation, policy verdicts, evidence emission |
| `pccp-scheduler` | `:8455` / `:8445` | Model traffic director. KV-cache-aware routing, prefill/decode disaggregated execution, canary rollout, region failover |
| `pccp-pia` | `:9090` / `:9444` | Inference agent. Verifies leases, then proxies requests to vLLM · SGLang |
| `pccp-bench` | — | F3 latency/streaming benchmark |
| `pccp-alert-backfill` | — | Migration tool that moves alert endpoint credentials to encrypted storage |

## What's Inside

Details live in the linked documents.

- **Three web consoles** — navigation and screens change entirely with the access profile:
  Patty Ops (service operations), Enterprise (customer governance), Account Portal (subscriber
  self-service). → [FEATURE_DOCUMENTATION](docs/FEATURE_DOCUMENTATION.md)
- **DARI protocol** — AI semantic v2 covering tool calling, structured output, multimodality,
  and cache accounting, plus a conformance suite independent implementations can pass. →
  [DARI.md](DARI.md) · [Protocol spec](docs/plans/DARI/DARI_Protocol_Specification_v1.0.md)
- **Model scheduler** — routing that knows where KV cache lives, prefill/decode disaggregation
  (P/D), staged rollout via shadow comparison and canary thresholds, region failover that honors
  data residency. → [PAT-1445 Router Evolution](docs/plans/2026-08-20-pat-1445-router-evolution-completion.md)
- **Enterprise governance** — org/project/repository hierarchy, DLP and policy epochs,
  Git-linked line-level human/AI attribution, audit evidence chains. →
  [API_REFERENCE](docs/API_REFERENCE.md)
- **Public cloud operations** — subscription and entitlement, fair scheduling via work slots,
  account integrity kept separate from T&S state, SRE console. →
  [PRD v2 §10C](docs/pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md)
- **Sovereign deployment** — local PKI/KMS, offline catalog and updates, air-gapped operation
  profile. → [PRD v2 §1.3](docs/pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md)

## Quick Start

You need Go 1.26+, Node.js 22+ with pnpm, and SQLite (dev default) or PostgreSQL 16+ (production).

```bash
make build        # pccp-server · pccp-relay · pccp-pia + web console
go build ./cmd/pccp-scheduler ./cmd/pccp-bench   # remaining binaries

make dev-server   # control plane :8080
make dev-relay    # relay :8090
make dev-pia      # PIA :9090 (mock engine)
```

Or with Docker:

```bash
cd deployments/docker && docker compose up
```

Then open http://localhost:8080 and bootstrap the initial admin.

- Email: `admin@patty.dev`
- Password: `changeme`

Both values can be changed via the `PCCP_ADMIN_EMAIL` and `PCCP_ADMIN_PASSWORD` environment variables.

## Development

```bash
go test ./...                        # full test suite
cd web && pnpm install && pnpm dev   # web dev server :8111 (proxies :8080)
```

Dev uses SQLite by default (under `.data/`). To switch to PostgreSQL:

```bash
export PCCP_DB_DRIVER=postgres
export PCCP_DB_DSN="host=localhost port=5432 user=pccp password=pccp dbname=pccp sslmode=disable"
```

## Repository Layout

```text
pccp/
├── cmd/                 six binaries — server · relay · pia · scheduler · bench · alert-backfill
├── internal/
│   ├── dari/            DARI protocol — CBOR, COSE-Sign1, QUIC/TCP transport, AI semantic v2
│   ├── scheduler/       model scheduler — routing, KV index, P/D split, canary, regions
│   ├── relay/ · pia/    data plane business logic
│   ├── models/ · db/    GORM domain models and database layer
│   ├── api/             REST handlers
│   ├── identity/ registry/ policy/ provenance/     kernel domains
│   └── publiccloud/ billing/ metering/ workintel/ sovereign/ …   profile modules
├── web/                 React admin console
├── conformance/         DARI conformance suite
├── adapters/            vLLM · SGLang adapters
├── sdk/                 PIA SDK and examples
├── registry/            protocol registries — messages · profiles · errors · crypto
├── deployments/         Docker · Kubernetes manifests
└── docs/                PRDs, specs, plans, API reference
```

## Documentation

| Document | Contents |
|---|---|
| [PRD v2.0](docs/pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md) | Full product requirements. Authoritative over v1 wherever they conflict |
| [Master Plan](docs/MASTER_PLAN.md) | Document navigation |
| [Current State](docs/CURRENT_STATE.md) | Current implementation status |
| [Implementation Status](docs/IMPLEMENTATION_STATUS.md) | Per-console-screen progress |
| [Feature Documentation](docs/FEATURE_DOCUMENTATION.md) | Complete console/harness feature set |
| [API Reference](docs/API_REFERENCE.md) | REST API |
| [PAT-1445 Router Evolution](docs/plans/2026-08-20-pat-1445-router-evolution-completion.md) | Scheduler evolution design and completion record |

## Operating Principles (non-negotiable)

1. **One product, three profiles.** No government code forks. Policy defaults and deployment
   topology differ; the code does not.
2. **Schema before UI.** Every entity defined in the PRD becomes a signed schema before any
   dashboard renders it.
3. **Vertical slices, not horizontal layers.** Every increment must run end-to-end through
   Harness → Relay → PIA → Control Plane, however small. No "build all the dashboards first".
4. **Evidence is part of the build.** Every protected action emits its event in the same commit
   that implements it. No retroactive "add logging later".
5. **Conformance is part of the protocol.** DARI ships with a conformance suite; the reference
   implementation must pass it, and independent implementations must be able to pass it too.
6. **The open-source boundary holds.** What should be open stays open; what should be proprietary
   stays proprietary. The trust boundary is signed model packages and endpoint attestation, not
   code secrecy.
7. **No phantom compliance.** Never claim "CSAP compliant", "KISA certified", or "ISMS-P
   certified" because a feature exists. We build mappings and evidence; certification is the
   customer's process.
8. **No HTTP/REST/WebSocket for protocol traffic.** The protocol binds QUIC and TLS/TCP. If the
   network blocks QUIC, fall back to TLS/TCP — never REST. The HTTP API is for administration only.
9. **Harness changes ship from the Harness repo.** patty-code is a separate repository. Do not
   stage Harness files into this one.
10. **No autonomous employment decisions.** Work Intelligence provides rubric scores with
    evidence, nothing further. Any consequential employment decision requires a human finalization step.
