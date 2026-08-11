# Patty Code Control Plane (PCCP)

The governance, identity, security, provenance, communication, and operational backbone of the Patty Code product line.

This repository is the home of three things:

1. **PCCP** — the enterprise control plane product (the main thing being built).
2. **PAPER** — the open communication protocol that the Harness and the Relay use to talk (Harness ↔ Relay ↔ PIA ↔ inference).
3. **The work plan and PRDs** — the source of truth for what "done" means.

The Harness binary lives in the [`patty-code`](https://github.com/patrickrho-patty/patty-code) repo. A worktree of that repo is checked out under `patty-code-pccp/` for convenience but is **gitignored** — it is not part of this repo, and changes to the harness land there, not here.

---

## If you are an agent: read this first

You are a long-running agent. You will work on this repo for weeks. Each time you start a session, follow this checklist **before** you do anything else:

1. **Read [`docs/MASTER_PLAN.md`](docs/MASTER_PLAN.md).** It is the navigation document for this repo. It tells you what exists, what does not, and what the immediate next step is.
2. **Skim the PRD [`docs/plans/Patty_Code_Control_Plane_PRD_v1.md`](docs/plans/Patty_Code_Control_Plane_PRD_v1.md).** You do not need to memorise every section, but you need to know what the product is, what it is not, and where the 14-point Definition of Done lives.
3. **Skim the protocol spec [`docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md`](docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md).** Anything you build that touches the wire must conform to this.
4. **Read [`docs/CURRENT_STATE.md`](docs/CURRENT_STATE.md) (if it exists).** After every phase, the prior session updates this file so the next session can pick up exactly where the last one left off.
5. **Read this file's _Conventions_ and _Guardrails_ sections.** Do not skip them.

If you do not see a `CURRENT_STATE.md`, the repo is empty. The first build slice is in §"First build slice" below — start there.

---

## Repo layout

```
/Users/patrickrho/projects/pccp           ← this repo (pccp)
├── README.md                             ← you are here
├── .gitignore                            ← excludes patty-code-pccp/
├── docs/
│   ├── MASTER_PLAN.md                    ← navigation doc, source of truth for "what next"
│   ├── CURRENT_STATE.md                  ← (create on first session) phase-by-phase status
│   ├── CHANGELOG.md                      ← (create on first session) running log of decisions
│   └── plans/
│       ├── Patty_Code_Control_Plane_PRD_v1.md     ← the product PRD
│       └── PAPER/
│           ├── PAPER_Comprehensive_PRD_v1.0.md            ← the protocol's PRD
│           ├── PAPER_Protocol_Specification_v1.0.md       ← the wire protocol (normative)
│           └── PAPER_arXiv_Paper_v1.0.md                  ← the research manuscript
├── src/                                  ← (Phase 0+) control plane implementation
├── relay/                                ← (Phase 0+) PAPER Relay data plane
├── pia/                                  ← (Phase 0+) Patty Inference Agent
├── proto/                                ← (Phase 0+) CBOR schema, message definitions, test vectors
├── conformance/                          ← (Phase 0+) PAPER conformance suite
├── deployments/                          ← (Phase 0+) deployment profiles (cloud / enterprise / sovereign)
└── patty-code-pccp/                       ← gitignored worktree of patty-code (the Harness)
```

The harness worktree is the place you modify the Harness binary. It is its own git repo with its own remote (`github.com:patrickrho-patty/patty-code`). Commit Harness changes there and push to *that* repo, not to `pccp`.

---

## Architecture (one-paragraph mental model)

```
developer machine                        PCCP                                         inference host
┌───────────────┐      PAPER (QUIC/TCP)  ┌──────────────────────────────┐              ┌────────────┐
│ Patty Code    │ ─────────────────────► │   Control Plane (admin)     │              │            │
│  Harness      │                        │ ┌──────────────────────────┐ │     PAPER    │   PIA      │
│  (HARNESS     │                        │ │   PAPER Relay (data)     │ │ ───────────► │ (INFERENCE)│
│   profile)    │                        │ │  auth · lease · policy   │ │              │            │
└───────────────┘                        │ │  DLP · verdict · evidence│ │              └─────┬──────┘
                                        │ └──────────────────────────┘ │                    │
                                        │                              │            localhost│
                                        └──────────────────────────────┘                    ▼
                                                                                       vLLM/SGLang
```

The Harness talks **only** to the Relay. The Relay is the only thing allowed to route to a PIA. PIA is the only thing allowed to call vLLM/SGLang. The Control Plane is authoritative for identity, policy, registry, audit, and provenance, but it is **not** in the hot token path.

The full protocol surface — Peer Profiles, Capability Leases, Policy Epochs, Relay Verdicts, Provenance Spine, Evidence Receipts — is defined in the protocol spec. Any code you write that touches the wire must conform to it.

---

## Current state (as of repo init)

Nothing is built yet. The PRDs are written; the code is not. Specifically:

- **No PCCP source code.** `src/`, `relay/`, `pia/`, `proto/`, `conformance/`, `deployments/` directories do not exist yet.
- **No PAPER protocol implementation.** The Harness today is wired to OpenAI-compatible endpoints via `internal/provider/openai` and `patty.example.toml` → `default_model = "patty/medium"` → `https://omni.agents.patty.io/v1`.
- **No Control Plane admin console.**
- **No enrolment, lease, or attestation infrastructure.**
- **No PIA** — there is no signed inference peer in front of vLLM.
- **No conformance suite, no test vectors, no schema definitions.**

The Harness worktree already has substantial *concepts* that line up with the protocol — `internal/capability`, `internal/evidence`, `internal/control`, `internal/remote`, `internal/provider` — but those are local-internal machinery. Your job is to make those concepts part of a live protocol exchange with PCCP.

---

## First build slice

This is the smallest slice that proves the architecture. **If only one thing ships in Phase 0, it is this.** Verbatim from the PRD Appendix H, reformatted as a checklist:

1. [ ] `Organization`, `User`, `Harness`, `Project`, `Repository`, `Session`, `Action` schemas (CBOR, signed).
2. [ ] User SSO + independent Harness enrollment / certificate.
3. [ ] Signed `ActionEnvelope` and audit stream.
4. [ ] `ModelPackage`, `InferenceEndpoint`, `EndpointAttestation`, `EndpointLease` schemas.
5. [ ] PIA in front of one vLLM deployment.
6. [ ] Signed Patty model manifest verification.
7. [ ] Relay that rejects any endpoint without a valid `EndpointLease`.
8. [ ] One repository baseline + one model request correlated end-to-end.
9. [ ] One code patch written as `ChangeSet` with provenance to user/Harness/model.
10. [ ] Minimal Control UI showing user, Harness, session, repo/branch, model endpoint, request, and signed timeline.

**The Phase 0 demo is:**

> An enterprise admin enrolls 김개발 and his managed Patty Code Harness, assigns him to Project A, permits only `Patty-KoCoder-v1`, starts a session on `repo/payment-service` branch `feature/refund`, sends one Korean coding request, routes it only to an attested Patty endpoint, records the resulting edit against exact Git state, and then opens Control to see the complete user → Harness → prompt → model → file → diff → policy → commit provenance chain.

When the agent onboards, it should:

1. Create `docs/CURRENT_STATE.md` with the above checklist as the starting point.
2. Pick the first item (usually schemas), discuss the data model briefly, then build it.
3. End every session by updating `docs/CURRENT_STATE.md` to reflect what was done, what is in flight, and what the next session should pick up.

---

## Conventions

### Git workflow

- **One branch.** `main`. Land small, vertical, end-to-end slices as commits. Do not maintain long-lived feature branches.
- **Commit messages.** Conventional commits. Use `feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`. The body explains *why*; the subject explains *what*.
- **One logical change per commit.** If a commit touches the protocol schema and the UI, it is two commits.
- **Push to `main` after every green commit.** The remote is `github.com:patrickrho-patty/pccp`. The Harness worktree is a separate remote.
- **Never amend a pushed commit.** Add a follow-up commit. Reviewers are reading history.
- **Never force-push to `main`** unless you have explicit, recent user approval and the PR is private.

### Repo hygiene

- **Keep `docs/` at the root of every change.** A schema change ships with a docs change. A protocol change ships with a test vector change. A new component ships with a section in `MASTER_PLAN.md`.
- **No file > 1,000 lines without a sub-file split.** Long files are a sign that the abstraction is wrong.
- **No placeholder code.** No `TODO: implement later` blocks. If the work is not in scope, do not write the code.
- **No dead code.** Things removed are removed in the same commit as the replacement.
- **`.gitignore` is for the worktree only.** Anything else gets committed unless it is obviously a build artifact.

### Harness integration

The Harness is a worktree at `patty-code-pccp/`. It is **gitignored** in this repo. To make a Harness change:

```bash
cd /Users/patrickrho/projects/pccp/patty-code-pccp
# make your changes
git -C /Users/patrickrho/projects/pccp/patty-code-pccp add -A
git -C /Users/patrickrho/projects/pccp/patty-code-pccp commit -m "..."
git -C /Users/patrickrho/projects/pccp/patty-code-pccp push
```

You can also enter the worktree and run normal git commands there. Just do **not** add Harness files to `pccp`. The worktree has its own CLAUDE.md / PATTY.md standing instructions — read those before making Harness changes.

When the harness needs to speak PAPER, the harness-side work happens in the worktree, and the protocol contract lives here in `pccp`. The two repos pull against each other across a contract, not through shared code.

### Code conventions

- **Language.** Go for the Relay, PIA, and the protocol library (the existing Harness is Go 1.25, see `go.mod`). Use the same idioms as the Harness for consistency. The Control Plane admin UI is TBD — decide as Phase 0 hits that surface.
- **Wire format.** Deterministic CBOR. COSE for signatures. QUIC first; native TLS 1.3/TCP fallback. No HTTP/REST/WebSocket for protocol traffic.
- **Cryptography.** No proprietary primitives. Use TLS 1.3, COSE, CBOR, HPKE/MLS where appropriate. Channel binding via RFC 9266 `tls-exporter`.
- **Identity.** Users, Harness instances, Devices, Model Endpoints, Model Packages, and Administrators are **all separate identities**. Authenticating one MUST NOT silently authenticate another.
- **Authority.** Time-bounded Capability Leases. The model grants itself no authority.
- **Provenance.** Causal DAG, content-addressed. Wall-clock ordering is supplemental, not primary.

---

## Guardrails (non-negotiable)

These are the rules that protect the product from becoming the thing the PRDs explicitly say it must not be. If you find yourself about to violate one of these, stop and surface it.

1. **One product, three profiles.** Never accept code that forks for Government. Different policy defaults, different deployment topology, same code.
2. **Schema before UI.** Every entity defined in the PRD becomes a signed schema before any dashboard renders it.
3. **Vertical slices, not horizontal layers.** Every increment must ship end-to-end through Harness → Relay → PIA → Control, even if the surface is tiny. No "build all the dashboards first".
4. **Evidence is part of the build.** Every protected action emits an event in the same commit the action is implemented. No retroactive "add logging later".
5. **Conformance is part of the protocol.** PAPER comes with a conformance suite. Reference implementation must pass it. Independent implementations must be able to pass it.
6. **Open-source boundary is enforced.** What is open stays open; what is proprietary stays proprietary. The control plane itself is open source (see PRD §9.9). The trust boundary is the signed model package and the endpoint attestation, not the secrecy of the code.
7. **No phantom compliance.** Do not claim "CSAP compliant", "KISA certified", or "ISMS-P compliant" because a feature exists. Maps and evidence are the product; the certifications are the customer's process.
8. **No HTTP/REST/WebSocket for protocol traffic.** The protocol binds QUIC and TLS/TCP. There is no "compat mode" for the Harness. If the network blocks QUIC, fall back to native TLS/TCP — never to REST.
9. **Harness changes ship from the worktree.** `patty-code-pccp/` is a separate repo. Do not stage Harness files into this repo.
10. **No employee evaluation autonomy.** Work Intelligence produces rubric scores with evidence. A human finalization step is required for any consequential employment decision. Period.

---

## Reference index

- **Master plan:** [`docs/MASTER_PLAN.md`](docs/MASTER_PLAN.md) — read this first.
- **Product PRD:** [`docs/plans/Patty_Code_Control_Plane_PRD_v1.md`](docs/plans/Patty_Code_Control_Plane_PRD_v1.md) — the customer-facing "what" and "why".
- **Protocol PRD:** [`docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md`](docs/PAPER/PAPER_Comprehensive_PRD_v1.0.md) — the protocol's "what" and "why".
- **Protocol spec:** [`docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md`](docs/PAPER/PAPER_Protocol_Specification_v1.0.md) — the normative wire contract.
- **Research manuscript:** [`docs/plans/PAPER/PAPER_arXiv_Paper_v1.0.md`](docs/PAPER/PAPER_arXiv_Paper_v1.0.md) — the design rationale and threat model.
- **Harness security/government baseline:** `patty-code-pccp/docs/GongCode_Master_Plan.md` (referenced by PRD §Appendix F as the security/government baseline that PCCP reuses).
- **Harness substrate:** `patty-code-pccp/internal/capability/`, `patty-code-pccp/internal/evidence/`, `patty-code-pccp/internal/control/`, `patty-code-pccp/internal/provider/`.

When the PRD and the README disagree, the PRD wins. When the protocol spec and the PRD disagree, the spec wins (it is normative). When this README and either disagree, the source-of-truth docs win — file an issue against this README.

---

## How to think about the work

This is a multi-component system that needs to ship **as a coherent product**, not as a collection of microservices. Three mental models will help:

1. **The Harness is a thin client.** It does not own policy, it does not own routing, it does not own secrets. It is an enrolled peer that asks for permission. If you find yourself adding policy logic to the Harness, you are probably working in the wrong layer.
2. **The Relay is the bottleneck by design.** One Relay, one rate-limit, one verdict chain. If you find yourself wanting to make the Relay faster by skipping a check, you are about to break the contract.
3. **The Control Plane is the source of truth.** If the Relay and the Control Plane disagree about who is allowed to do what, the Control Plane wins. The Relay holds cached, signed state; the Control Plane is authoritative.

The biggest failure mode is letting the architecture drift into a generic LLM gateway. The PRDs explicitly say CP is not a LiteLLM/Bifrost competitor. The product is governance, security, provenance, communications, and work intelligence, applied to AI engineering. The gateway exists to serve that — not the other way around.

If you are unsure whether a feature is in scope, the answer is usually in PRD §4.4 (Non-Goals) or §52 (Explicit Non-Goals for Initial Releases). Read those before adding anything new.
