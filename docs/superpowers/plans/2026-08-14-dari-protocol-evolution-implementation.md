# DARI Protocol Evolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver DARI as a complete, enterprise-ready open protocol and product runtime: publication-quality English/Korean papers, a compatibility-preserving secure data plane, browser and cross-domain bindings, wired control-plane enforcement, governed collaboration/tooling, conformance, operations, and the final repository-wide rename.

**Architecture:** Reuse the existing framing, deterministic CBOR, COSE, TLS/TCP, QUIC, connection state, Relay, inference adapters, control-plane services, and registries. Deepen the DARI kernel—Peer Credential, Authorization Grant, Governed Exchange, Authorization Decision, Evidence Receipt, and transactional effects—behind neutral interfaces, then wire those interfaces into every live request path. Add WebTransport/HTTP/3 and constrained WebSocket browser binding, bilateral federation, durable hot-path state, enterprise operations, and governed collaboration as executable profiles. Preserve `paper/1` only in a legacy adapter while all active packages, comments, documentation, UI copy, and publication artifacts move to DARI.

**Tech Stack:** Go 1.26, `fxamacker/cbor`, COSE-Sign1, Ed25519, TLS 1.3, `quic-go`, GORM, LaTeX/XeLaTeX, BibTeX, vLLM/SGLang adapters.

## Global Constraints

- Work directly on the current forge branch; do not create a branch or worktree.
- Preserve all uncommitted user work. Stage exact task-owned paths only; never use `git add -A` or `git add .`.
- At the start of every task, save `git status --short` and `git diff -- <task paths>` under `/tmp/dari-task-N-before.patch`. For a path that was already dirty, use `git add -p` to stage only task-owned hunks. If task-owned hunks cannot be isolated safely, leave the task uncommitted and record that fact instead of committing the user's pre-existing edits.
- Immediately before each root source commit, run `git diff --cached --name-only -- '*.go' >> /tmp/dari-owned-go-files.txt`. Immediately before each Patty Code source commit, run `git -C patty-code-pccp diff --cached --name-only -- '*.go' >> /tmp/dari-patty-code-owned-go-files.txt`. The final formatting gates format only these allowlists.
- Never modify `.env`, `.env.local`, `.env.*`, secrets files, or credential-bearing configuration.
- Required order: arXiv sources/PDFs, protocol behavior, repository-wide rename.
- Do not rewrite working framing, CBOR, COSE, transport, Relay/PIA adapter, or registry code merely to rename it.
- Existing numeric message IDs and legacy deterministic encodings remain stable.
- New connections advertise `dari/1`; `paper/1` remains only in the legacy adapter and its fixtures.
- DARI expands to **Delegated Authorization and Receipts for Inference**.
- Credit **Patty Co., Ltd.** as originator and principal steward; never use “Patty Research.”
- Korean prose must read as native research writing. Introduce `위임 인가(delegated authorization)` once, then use `권한 위임`; never translate protocol time as `시계`.
- No paper claim may describe an unimplemented property as measured or enforced.
- Full-delivery rule: no advertised DARI profile may remain schema-only, documentation-only, generated-code-only, feature-flag-only, deferred, or experimental at the project release gate. Each advertised profile requires a runtime path, durable state where applicable, operator controls, telemetry, failure semantics, black-box conformance, and deployment evidence.
- Runtime-first enterprise scope: browser access, cross-domain federation, inference/tool/model-supply adapters, collaboration/media, catalog/policy enforcement, metering, provenance, compliance, onboarding, and sovereign operations are implementation tracks in this plan—not future placeholders.
- A capability may be marked `UNSUPPORTED` during development, but only until its named runtime and conformance tasks below pass. `UNSUPPORTED` is a development/release-readiness result, never a way to defer a promised release capability. The release gate must not ship a profile whose only evidence is a schema or registry row.
- Generated LaTeX outputs are rebuilt, never hand-edited.
- Every behavior change starts with a failing test and ends with targeted tests plus `go test ./...`.

## Gap-closure map

The older paper, product, and protocol plans remain historical references. Their unfinished items are absorbed into this plan as executable work:

| Existing wording or gap | Closed by this plan | Required runtime evidence |
|---|---|---|
| `implementation-required` peer authentication | Task 6 | transcript-bound proof, revocation termination, black-box negatives |
| `implementation-required` authorization and attenuation | Tasks 7–8 | signed grants, parent-chain validation, broadening rejection matrix |
| `implementation-required` decisions, freshness, receipts, selective disclosure | Tasks 9–10 | deny-overrides, checkpoint high-water marks, multi-party receipts, MMR proofs |
| `implementation-required` transactional effects | Task 11 | durable prepare/authorize/execute/commit/abort/status and reconnect tests |
| `dari.web/1` previously schema-only | Task 13 | WebTransport/HTTP/3 runtime, constrained WebSocket fallback, browser proof-of-possession, origin binding, reconnect and effect-status tests |
| `dari.federation/1` previously schema-only | Task 14 | trust-bundle discovery, bilateral issuer/audience validation, policy intersection, residency enforcement, cross-domain receipt verification |
| dead or partially wired 14-stage governance path | Task 15 | live Harness→Relay→PIA pipeline, hot-state cache, catalog/PMP/endpoint chain, DLP, scheduler, metering, evidence, backpressure |
| enterprise stubs and declarative-only controls | Task 16 | SSO/SCIM, hierarchy/delegated admin, KMS/HSM seam, compliance evidence, retention/legal hold, onboarding/migration, rollout/rollback, KPIs |
| tool/network/secret, SCM, sandbox, and provenance gaps | Task 17 | governed MCP/tool/network/secret calls, real Git/SCM/change-set binding, isolated sandbox executor, provenance-fed live path |
| chat/presence/file/voice and non-compilable SDK artifacts | Task 18 | DARI collaboration/media streams, encrypted delivery, resumable scanned files, browser/Go SDKs, provider capability adapters |
| proxy or comment-only conformance tests | Task 19 | capability manifests, canonical/negative vectors, stateful black-box runner, two-implementation interoperability |
| previously future-labeled live audio, connectors, approved engines, and standards adapters | Tasks 12, 17–19 | versioned runtime profiles with explicit capability reporting and integration tests; no unimplemented feature is advertised |

The following earlier planning documents are retained as publication history, not independent execution sources. Any unfinished implementation language in them is superseded by this closure map and Tasks 1–24: `docs/superpowers/plans/2026-08-12-paper-arxiv-publication-plan.md`, `docs/superpowers/plans/2026-08-12-paper-benchmark-mathematics-implementation.md`, `docs/superpowers/plans/2026-08-12-paper-korean-edition-implementation.md`, `docs/superpowers/plans/2026-08-12-paper-visual-evidence-implementation-plan.md`, `docs/superpowers/plans/2026-08-13-paper-product-positioning-implementation.md`, and `docs/superpowers/plans/2026-08-13-paper-professional-rhetoric-audit.md`.

## Current implementation status (2026-08-15, closing update)

22 of 24 tasks complete. Per-task status:
- Tasks 1–20: COMPLETE (papers, normative contract, legacy freeze, kernel objects F.2–F.14, profile machinery, web/federation/collab/media runtimes, live hot path with stage enforcement + no-mock-fallback + event spine, enterprise controls incl. exception workflow + rollout, executable tool/SCM/sandbox/connector boundaries, black-box conformance runner + manifest.json, rename).
- Task 21–22: COMPLETE (connector + repository-wide DARI rename with frozen legacy surface, registry reconciliation, gates enforced).
- Task 23 (open-spec governance): BLOCKED at the license decision gate — Step 1 requires an explicit license choice by the steward; attribution/contribution/compatibility mechanics draft awaits that decision.
- Task 24 (release gate): PENDING — run after Task 23.

Deployment-evidence debts (recorded honestly in conformance/manifest.json as DEGRADED omissions, all non-critical): browser deployment evidence (dari.web/1), partner interconnect (dari.federation/1), live multi-peer collab/media deployments, tokenizer-exact accounting (estimator is marked estimated), PIA-side PMP load verification.

## Current implementation status

This is the release-readiness source of truth. **Status update (2026-08-15):**

- **Tasks 1–5: complete** (papers, normative appendix, legacy byte freeze; golden vectors re-pinned after the rename).
- **Task 6: complete on the live path.** Transcript-bound AUTH_PROOF verified end-to-end against the real relay binary (enrollment via `/v1/enroll`, CA trust bundle injected in `cmd/pccp-relay`, canonical map-order-free HELLO/ACK transcript encodings, RFC-8152 array-form Sig_structure); revocation propagation wired (`Service.RevokeHarness` → `ApplyRevocationSnapshot` → active-stream termination + epoch advance, admin endpoint, reconnect-termination evidence in the live e2e). Residual: standalone Appendix-F.3 fixture *files* (behavior is pinned by conformance + live tests).
- **Task 7 (signed Authorization Grant): implemented** — F.4 object, canonical signing, scope normalization, session-grant issuance on SESSION_GRANT, legacy-lease adapter, GovernInference grant verification (fail-closed). Live e2e asserts the grant.
- **Task 8 (attenuation): implemented** — full 10-step F.4 algorithm with the required broadening-rejection matrix (14 negative cases), replay ledger, IssueChildGrant.
- **Task 9 (decisions/obligations/checkpoints): implemented** — F.6 decision + aggregation + obligation state machine; F.7 checkpoint + freshness + rollback ledger; live ALLOW/DENY decision issuance per governed exchange.
- **Task 10 (receipt attestations + selective disclosure): implemented** — F.8 attestation scope rules; F.9 segmented-MMR commitment + disclosure prover/verifier (tamper/duplicate/geometry rejections).
- **Task 11 (transactional effects): implemented** — F.10 object family + executor state machine (idempotency, REPLAY_CONFLICT, terminal freeze, retry-owner, status shapes).
- **Task 21+22 (rename): complete** — repository-wide PAPER→DARI with frozen legacy surface in `legacy_paper1.go`, registry reconciliation, legacy-allowlist gate enforced, arXiv artifacts rebuilt as DARI_*.
- **Profile negotiation machinery (map §3/F.13): implemented** — kernel + implemented extensions register as passed; web/federation/collab/media correctly return UNSUPPORTED with recorded reasons.
- **Tasks 13, 14, 18 (web/federation/collab/media runtimes): NOT implemented** — and per the map, schema-only release is forbidden; they remain UNSUPPORTED until their named vectors exist.
- **Tasks 15–17 (pipeline hardening, enterprise controls, tools/SCM/sandbox live wiring): partial** — live DLP, session-status enforcement, heartbeats, audit hash chain, provenance ingestion, and governed streaming are wired; the full 14-stage pipeline hardening, SSO/SCIM, KMS/HSM, retention, and real SCM/sandbox runtimes remain open.
- **Task 19 (black-box conformance runner): partial** — kernel-level negative matrices exist (attenuation, checkpoints, decisions, effects, disclosure, negotiation); the stateful two-implementation runner does not.
- **Task 24 (release gate): not passed.

The plan never converts a schema, registry entry, feature flag, generated client, or documentation row into product support. Every task below ends with executable behavior, durable state where needed, operator controls, telemetry, negative tests, and a commit in the owning repository.

## Enterprise coverage matrix

The following known gaps are explicit work, not out-of-scope notes. Each row must have an implementation, an end-to-end test, and an operator/deployment record before Gate C can pass.

| Existing gap inventory | Full-product owner | Required evidence |
|---|---|---|
| Live 14-stage pipeline, PAPER-only path, metering/evidence, fleet revocation/catalog chain, PMP signature/hash, catalog push, inline DLP/injection, scheduler, tokenizer/structured AI output, no mock inference fallback, PIA attestation | Task 15 | live request traces, provider admission tests, signed catalog/model/endpoint chain, usage/evidence receipts, failure/backpressure tests |
| SSO/SAML/OIDC, SCIM, organization hierarchy, delegated administration, ABAC/policy acknowledgement/exception/epochs, CSAP/ISMS-P evidence, KMS/HSM, retention/legal hold, offline updates, onboarding/migration, rollout/rollback/HA, KPIs and observability, work-intelligence dispute/bias/gaming controls, console privacy boundary, global search, payment/billing, wallboard and historical comparison, Korean enterprise differentiators | Task 16 | executable control-plane flows, access-denial tests, audit/compliance exports, operator runbooks, dashboards and migration/rollback evidence |
| Tools/MCP/network/secrets, SCM/change-set binding, sandbox isolation, connectors, live provenance, model-supply and hardware attestation/reattestation seams | Task 17 | executor denial-before-side-effect tests, isolated runtime tests, signed change/provenance receipts, attestation/key-release tests |
| Governed chat/presence/broadcast, E2E encrypted voice/media, resumable scanned files, compilable Go/web SDKs, live UI streams, wallboard/history integration | Task 18 | encrypted ordered delivery, file scan/retention receipts, SDK compile/interoperability tests, live UI evidence |
| IA.2–IA.6 privacy/search/payment/wallboard surfaces; X.1–X.12 semantic contract, chargeback, GPU telemetry, event spine, retention, SDK, reporting, config, onboarding, acceptance, KPIs; P.2–P.4 attestation/reattestation/mock fallback; 5.3 tokenizer; 9.3/10.3 work-intelligence and privacy enforcement; Korean differentiators 33.1–33.15 | Tasks 15–18 | each item linked to a named test and deployment artifact; unresolved rows fail the release gate |

---

## Phase 1 — arXiv Paper and PDFs

### Task 1: Establish the English DARI paper and claim matrix

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`
- Modify: `docs/plans/PAPER/arxiv/references.bib`
- Regenerate: `docs/plans/PAPER/arxiv/figures/qwen-governed-exchange.png`
- Regenerate: `docs/plans/PAPER/arxiv/figures/governed-exchange-lifecycle.png`
- Modify: `docs/plans/PAPER/arxiv/figures/model-identity-chain.tex`
- Modify: `docs/plans/PAPER/arxiv/README.md`
- Create: `docs/plans/PAPER/arxiv/implementation-claim-matrix.tsv`

**Interfaces:**
- Consumes: current benchmark TSV and invariant audit.
- Produces: authoritative terminology and evidence status for Task 2 and Phase 2.

- [x] **Step 1: Create the claim matrix**

```tsv
claim_id	claim	status	evidence
C1	Deterministic framing and canonical CBOR	implemented	internal/paper tests
C2	TLS/TCP and QUIC bindings	implemented	internal/paper transport tests
C3	Transcript-bound peer authentication	implementation-required	internal/relay/peer_authenticator_test.go
C4	Canonical signed authorization scope	implementation-required	internal/paper/authorization_test.go
C5	Delegated authorization attenuation	implementation-required	internal/paper/delegation_test.go
C6	Deterministic final receipt	implementation-required	internal/paper/receipt_test.go
C7	Browser binding	extension-profile	dari.web/1 vectors
C8	Cross-domain federation	extension-profile	dari.federation/1 vectors
```

- [x] **Step 2: Rewrite the title and kernel description**

Use `\title{DARI: Delegated Authorization and Receipts for Governed AI Inference}`. Define Credential, Authorization Grant, Governed Exchange, and Evidence Receipt as the kernel. Move PIA/PMP into the Patty reference profile and use Inference Peer/Model Artifact Manifest in the neutral core.

- [x] **Step 3: Add the compatibility lineage without co-branding**

Use: “DARI evolved from Patty Co., Ltd.'s earlier internal protocol implementation. The evaluated compatibility profile preserves its deployed record and message semantics while the open specification generalizes authorization and receipt interfaces.”

- [x] **Step 4: Add the attenuation equation**

```latex
G_i = \operatorname{Sign}_{K_i}(\mathrm{iss},\mathrm{sub},\mathrm{aud},\mathrm{cnf},\mathrm{scope}_i,t_{nbf},t_{exp},H(G_{i-1})),
\qquad \mathrm{scope}_i \subseteq \mathrm{scope}_{i-1}.
```

Retain existing primitive benchmarks as primitive measurements; do not portray them as delegation, browser, or federation measurements.

- [x] **Step 5: Preserve the evidence package and regenerate stale conceptual graphics**

Retain the existing four figures, three empirical/comparison tables, equations, B0--B4 baseline design, and checked-in benchmark dataset. Regenerate the two raster conceptual figures with the previously selected Qwen image model so they use DARI and the neutral kernel vocabulary; do not hand-draw replacements. The overview currently contains embedded `PAPER PATH` and `PAPER RELAY` labels, while the lifecycle graphic still embeds lease/PMP-era terms. Treat both as stale publication artifacts. Generated figures remain conceptual and must not depict unmeasured performance or implementation coverage. If regeneration would incur new paid API usage beyond the already approved graphics work, obtain explicit execution-time consent before invoking it.

- [x] **Step 6: Verify terminology and commit**

```bash
rg -n 'Patty Research|prototype|MVP|시계' docs/plans/PAPER/arxiv/main.tex
rg -n 'DARI|Delegated Authorization|Evidence Receipt|compatibility profile' docs/plans/PAPER/arxiv/main.tex
git add -p docs/plans/PAPER/arxiv/main.tex docs/plans/PAPER/arxiv/references.bib docs/plans/PAPER/arxiv/figures/model-identity-chain.tex docs/plans/PAPER/arxiv/README.md
git add docs/plans/PAPER/arxiv/figures/qwen-governed-exchange.png docs/plans/PAPER/arxiv/figures/governed-exchange-lifecycle.png
git add docs/plans/PAPER/arxiv/implementation-claim-matrix.tsv
git commit -m "docs: evolve arxiv paper from PAPER to DARI"
```

Expected: the prohibited-term search is empty and all DARI concepts are present.

### Task 2: Rewrite the Korean edition as native research prose

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main_ko.tex`
- Modify: `docs/plans/PAPER/arxiv/figures/model-identity-chain-ko.tex`

**Interfaces:**
- Consumes: Task 1 structure, claims, equations, and citations.
- Produces: a Korean edition with identical technical meaning and evidence status.

- [x] **Step 1: Use the Korean title and definition**

```latex
\title{DARI: 거버넌스 기반 AI 추론을 위한 위임 인가와 검증 가능 실행 증거}
```

Define: `위임 인가(delegated authorization)는 권한 주체가 보유한 권한의 일부를 명시적인 범위와 조건에 따라 다른 참여자에게 부여하는 검증 가능한 관계를 의미한다. 이하에서는 이를 권한 위임이라 한다.`

- [x] **Step 2: Apply this terminology consistently**

```text
Authorization Grant = 인가 증서
Governed Exchange = 거버넌스 적용 교환
Evidence Receipt = 검증 가능 실행 증거
Inference Peer = 추론 피어
Model Artifact Manifest = 모델 아티팩트 명세서
Policy Epoch = 정책 에포크
```

- [x] **Step 3: Verify parity and commit**

```bash
rg -n '시계|프로토타입|MVP|Patty Research' docs/plans/PAPER/arxiv/main_ko.tex
test "$(rg -c '^\\section' docs/plans/PAPER/arxiv/main.tex)" = "$(rg -c '^\\section' docs/plans/PAPER/arxiv/main_ko.tex)"
test "$(rg -c '^\\begin\{equation\}|^\\begin\{align' docs/plans/PAPER/arxiv/main.tex)" = "$(rg -c '^\\begin\{equation\}|^\\begin\{align' docs/plans/PAPER/arxiv/main_ko.tex)"
git add -p docs/plans/PAPER/arxiv/main_ko.tex docs/plans/PAPER/arxiv/figures/model-identity-chain-ko.tex
git commit -m "docs: publish native Korean DARI manuscript"
```

### Task 3: Build and visually inspect both PDFs

**Files:**
- Modify: `docs/plans/PAPER/arxiv/Makefile`
- Regenerate: `docs/plans/PAPER/arxiv/PAPER_arXiv.pdf`
- Regenerate: `docs/plans/PAPER/arxiv/PAPER_arXiv_KO.pdf`
- Regenerate: `docs/plans/PAPER/arxiv/PAPER-arXiv-source.tar.gz`

**Interfaces:**
- Consumes: Tasks 1–2.
- Produces: reproducible English and Korean publication artifacts before source changes.

- [x] **Step 1: Add a strict verification target**

```make
EN_TARGET := PAPER_arXiv
KO_TARGET := PAPER_arXiv_KO
ARCHIVE := PAPER-arXiv-source.tar.gz
COMMON_DEPS := references.bib $(wildcard figures/*) benchmark-data/core-primitives.tsv

.PHONY: all verify clean archive

all: $(EN_TARGET).pdf $(KO_TARGET).pdf

$(EN_TARGET).pdf: main.tex $(COMMON_DEPS)
	$(LATEXMK) -pdf -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(EN_TARGET) main.tex

$(KO_TARGET).pdf: main_ko.tex $(COMMON_DEPS)
	$(LATEXMK) -xelatex -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(KO_TARGET) main_ko.tex

verify: all
	@test -s $(EN_TARGET).pdf
	@test -s $(KO_TARGET).pdf
	! grep -E "Undefined control sequence|Citation .* undefined|Reference .* undefined|There were undefined references|Overfull \\hbox" $(EN_TARGET).log $(KO_TARGET).log

archive: verify
	tar -czf $(ARCHIVE) main.tex main_ko.tex references.bib Makefile README.md figures benchmark-data

clean:
	$(LATEXMK) -C -jobname=$(EN_TARGET) main.tex
	$(LATEXMK) -C -xelatex -jobname=$(KO_TARGET) main_ko.tex
```

- [x] **Step 2: Build and render every page**

```bash
make -C docs/plans/PAPER/arxiv clean
make -C docs/plans/PAPER/arxiv verify
make -C docs/plans/PAPER/arxiv archive
mkdir -p /tmp/dari-paper-en /tmp/dari-paper-ko
pdftoppm -png -r 120 docs/plans/PAPER/arxiv/PAPER_arXiv.pdf /tmp/dari-paper-en/page
pdftoppm -png -r 120 docs/plans/PAPER/arxiv/PAPER_arXiv_KO.pdf /tmp/dari-paper-ko/page
```

Inspect every figure, equation, table, reference page, and appendix for clipping, missing glyphs, arrow overlap, overflow, and untranslated Korean body prose.

- [x] **Step 3: Commit**

```bash
git add docs/plans/PAPER/arxiv/Makefile docs/plans/PAPER/arxiv/PAPER_arXiv.pdf docs/plans/PAPER/arxiv/PAPER_arXiv_KO.pdf docs/plans/PAPER/arxiv/PAPER-arXiv-source.tar.gz
git commit -m "docs: build DARI English and Korean papers"
```

---

## Phase 2 — Protocol Behavior

### Task 4: Freeze the normative DARI contract before changing source

**Files:**
- Modify: `docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md`
- Modify: `docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md`
- Create: `docs/plans/PAPER/DARI_COMPATIBILITY_AND_PROFILE_MAP.md`

**Interfaces:**
- Consumes: the reviewed English/Korean paper and implementation claim matrix.
- Produces: normative schemas, invariants, compatibility rules, and profile boundaries consumed by Tasks 5–19.

- [x] **Step 1: Define the neutral kernel and Patty reference profile**

Specify Peer Credential, Authorization Grant, Governed Exchange, Authorization Decision, and Evidence Receipt as neutral objects. Define Governance Relay, Inference Peer, Effect Executor, and Evidence Verifier as roles. Keep Harness, PIA, PMP, Patty Code, vLLM, and SGLang in a named Patty reference profile rather than the core.

- [x] **Step 2: Add complete normative schemas and validation algorithms**

Add deterministic CDDL for Authorization Grant, parent binding, attenuation, policy decisions/obligations, signed state checkpoints, receipt body/attestations, selective-disclosure proof, transactional effect messages, browser origin/proof/reconnect, federation trust bundles/policy intersections, collaboration envelopes, media chunks, and resumable file transfers. Specify fail-closed behavior, validation order, stable message allocations, operator configuration, and required negative conformance cases.

- [x] **Step 3: Define compatibility and extension boundaries**

The map must state:

```text
paper/1 record and message encoding -> DARI legacy compatibility profile
dari/1 kernel -> active protocol
dari.ai/1 -> provider-neutral inference
dari.tools/1 -> effects and tool bridges
dari.model-supply/1 -> model artifact and endpoint authorization
dari.web/1 -> browser/WebTransport and constrained WebSocket runtime profile
dari.federation/1 -> bilateral cross-domain trust and policy-intersection runtime profile
dari.collab/1 -> governed chat, presence, broadcast, encryption, and file-transfer runtime profile
dari.media/1 -> governed voice/live-media runtime profile
```

The spec must define wire behavior, persistence, failure modes, operator configuration, and conformance vectors for web and federation. During development a runtime may return `UNSUPPORTED` until Tasks 13–14 pass; schema publication alone must never upgrade the result to `EXACT` or `DEGRADED`.

- [x] **Step 4: Verify every Phase 2 implementation object has a normative section**

```bash
rg -n 'Authorization Grant|attenuation|Authorization Decision|Signed State Checkpoint|Receipt Attestation|selective disclosure|Effect Prepare|dari\.ai/1|dari\.web/1|dari\.federation/1|dari\.collab/1|dari\.media/1|profile-negotiation|obligation-update|auth-proof' docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md docs/plans/PAPER/DARI_COMPATIBILITY_AND_PROFILE_MAP.md
git diff --check -- docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md docs/plans/PAPER/DARI_COMPATIBILITY_AND_PROFILE_MAP.md
```

- [x] **Step 5: Commit only the normative contract hunks**

```bash
git add -p docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md
git add docs/plans/PAPER/DARI_COMPATIBILITY_AND_PROFILE_MAP.md
git commit -m "docs: define the normative DARI protocol contract"
```

### Task 5: Freeze current PAPER v1 wire behavior

**Files:**
- Create: `internal/paper/compatibility_test.go`
- Create: `conformance/testdata/paper1/hello.cbor.hex`
- Create: `conformance/testdata/paper1/record.bin.hex`

**Interfaces:**
- Produces byte-for-byte fixtures that Task 19 must continue to decode.

- [x] **Step 1: Write a golden HELLO test**

```go
func readFixture(t *testing.T, path string) string {
    t.Helper()
    data, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    return string(data)
}

func TestLegacyPaper1HelloGoldenVector(t *testing.T) {
    wantHex := strings.TrimSpace(readFixture(t, "../../conformance/testdata/paper1/hello.cbor.hex"))
    got, err := paper.MarshalCBOR(paper.HelloMessage{CoreVersions: []uint8{1}, PeerProfile: paper.ProfileHarness, ClientNonce: make([]byte, 32)})
    if err != nil { t.Fatal(err) }
    if hex.EncodeToString(got) != wantHex { t.Fatal("legacy HELLO changed") }
}
```

- [x] **Step 2: Capture current bytes and commit**

Run the test once to print the canonical bytes, store the exact hex in the fixtures, remove diagnostic output, then run:

```bash
go test ./internal/paper -run LegacyPaper1 -count=1
git add internal/paper/compatibility_test.go conformance/testdata/paper1
git commit -m "test: freeze PAPER v1 compatibility vectors"
```

### Task 6: Enforce transcript-bound authentication and revocation

**Files:**
- Create: `internal/relay/peer_authenticator.go`
- Create: `internal/relay/peer_authenticator_test.go`
- Modify: `internal/relay/paper_listener.go`
- Modify: `internal/paper/peer.go`
- Modify: `internal/paper/client.go`
- Modify: `internal/identity/auth.go`
- Modify: `internal/identity/service.go`
- Modify: `patty-code-pccp/internal/paperproto/messages.go`
- Modify: `patty-code-pccp/internal/paperproto/transport.go`
- Modify: `patty-code-pccp/internal/provider/paper/paper.go`
- Modify: `patty-code-pccp/internal/provider/provider.go`
- Modify: `patty-code-pccp/cmd/patcode/main.go`
- Modify: `patty-code-pccp/internal/config/config.go`

**Interfaces:**
- Produces `TrustBundle`, `NewPeerAuthenticator(TrustBundle)`, and `VerifyPeerProof(ctx context.Context, transcript []byte, proof *paper.AuthProofMessage) (*paper.PeerCredential, error)`.
- Test helper: `signedProofFixture(t *testing.T, transcript []byte) (*paper.AuthProofMessage, relay.TrustBundle)` creates an issuer, a signed credential, and a proof bound to the supplied transcript.

- [x] **Step 1: Write valid, tampered, replayed, expired, and revoked proof tests**

```go
func TestPeerAuthenticatorRejectsAnotherTranscript(t *testing.T) {
    proof, trust := signedProofFixture(t, []byte("transcript-a"))
    verifier := relay.NewPeerAuthenticator(trust)
    if _, err := verifier.VerifyPeerProof(context.Background(), []byte("transcript-b"), proof); err == nil { t.Fatal("replayed proof accepted") }
}
```

- [ ] **Step 2: Implement the verifier and reconcile the wire binding**

Decode the COSE credential; verify issuer signature, validity, protocol version, and revocation epoch; verify proof of possession over the canonical transcript hash, negotiated profile/transport binding, challenge ID, and credential digest. `PeerCredential.VerifySignature` must decode the COSE object and compare its payload byte-for-byte with `Credential.SigningBytes()` after signature verification; a valid signature over a different credential payload must fail. Remove `harnessID := string(proof.Credential)` and derive identity only from the verified credential. Reconcile the transcript, epoch-evidence, and credential-body encodings with Appendix F.3 and add the exact CDDL/golden vectors before enabling the live listener.

- [ ] **Step 3: Terminate active sessions by revoked serial and verify propagation**

```bash
go test ./internal/paper ./internal/identity ./internal/relay -run 'Auth|Peer|Revok|Transcript' -count=1
(cd patty-code-pccp && go test ./internal/paperproto/... -count=1)
git add internal/relay/peer_authenticator.go internal/relay/peer_authenticator_test.go
git add -p internal/relay/paper_listener.go internal/paper/peer.go internal/paper/client.go internal/identity/auth.go internal/identity/service.go
(cd patty-code-pccp && git add -p internal/paperproto/messages.go internal/paperproto/transport.go)
git -C patty-code-pccp diff --cached --name-only -- '*.go' >> /tmp/dari-patty-code-owned-go-files.txt
git -C patty-code-pccp commit -m "feat: present transcript-bound protocol credentials"
git diff --cached --name-only -- '*.go' >> /tmp/dari-owned-go-files.txt
git commit -m "feat: enforce transcript-bound peer authentication"
```

The identity revocation snapshot must be pushed to every active Relay listener (not merely stored in the identity service), and the Patty Code client must load its enrolled credential/private key and construct the real proof rather than sending an identifier placeholder. Add a reconnect test that revokes a serial in the control plane and observes the existing stream terminate before the next protected message.

- [ ] **Step 4: Close the release-blocking client and wire-vector gaps**

Replace every placeholder credential/signature byte in the nested provider with an enrolled Peer Credential and Ed25519 private-key load from the configured secure provider. Inject signed trust bundles into every Relay listener, propagate revocation checkpoints to `RevokeCredential`, and add byte-exact Appendix-F.3 vectors for the credential body, challenge, transcript hash, channel binding, COSE protected headers, `Sig_structure`, and proof carrier. This step is incomplete until the nested repository is cleanly committed and the root and nested black-box tests observe the same proof semantics.

### Task 7: Introduce a completely signed Authorization Grant

**Files:**
- Create: `internal/paper/authorization.go`
- Create: `internal/paper/authorization_test.go`
- Modify: `internal/models/model_registry.go`
- Modify: `internal/policy/service.go`
- Modify: `internal/relay/service.go`
- Modify: `internal/relay/paper_listener.go`
- Modify: `internal/relay/service_test.go`

**Interfaces:**
- Produces `AuthorizationGrant`, `AuthorizationScope`, `SigningBytes()`, `Sign()`, and `Verify()`.
- Test helper: `signedGrantFixture(t *testing.T) (*paper.AuthorizationGrant, []byte, ed25519.PublicKey)` creates a minimally scoped grant and returns its COSE signature and issuer key.

- [ ] **Step 1: Define the versioned object**

```go
type AuthorizationGrant struct {
    Version uint16 `cbor:"1,keyasint"`; GrantID string `cbor:"2,keyasint"`; Issuer string `cbor:"3,keyasint"`
    SubjectPeerID string `cbor:"4,keyasint"`; SubjectKeyThumbprint []byte `cbor:"5,keyasint"`; Audience []string `cbor:"6,keyasint"`
    OrganizationID string `cbor:"7,keyasint"`; UserID string `cbor:"8,keyasint"`; SessionID string `cbor:"9,keyasint"`; PolicyEpochID string `cbor:"10,keyasint"`
    Scope AuthorizationScope `cbor:"11,keyasint"`; NotBeforeMs int64 `cbor:"12,keyasint"`; NotAfterMs int64 `cbor:"13,keyasint"`; Sequence uint64 `cbor:"14,keyasint"`
    ParentGrantDigest []byte `cbor:"15,keyasint,omitempty"`; MaxDelegationDepth uint8 `cbor:"16,keyasint,omitempty"`
}
```

- [ ] **Step 2: Write scope mutation tests**

```go
func TestAuthorizationSignatureCoversScope(t *testing.T) {
    grant, signature, pub := signedGrantFixture(t)
    grant.Scope.AllowedTools = append(grant.Scope.AllowedTools, "shell.admin")
    if err := grant.Verify(pub, signature); err == nil { t.Fatal("expanded scope verified") }
}
```

- [ ] **Step 3: Implement canonical CBOR signing and persistence**

Sign every scope field. Add `GrantVersion`, `CanonicalGrantCBOR`, and `ParentGrantDigest` to the existing persisted lease model without deleting old columns. New issuance writes normalized query fields plus canonical bytes.

- [ ] **Step 4: Require an explicit grant in live governance**

Change the entry point to `GovernInference(ctx context.Context, req GovernRequest, grant *paper.AuthorizationGrant)` and update all known callers in `internal/relay/paper_listener.go` and `internal/relay/service_test.go`. The listener decodes the presented grant and never looks up an arbitrary active lease by Harness ID. During the compatibility window, legacy persisted leases are converted by one explicit `DecodeLegacyCapabilityLease` adapter before entering this function. Verify subject key, audience, session, epoch, action, model, budget, validity, and revocation before routing.

- [ ] **Step 5: Test and commit**

```bash
go test ./internal/paper ./internal/policy ./internal/relay -run 'AuthorizationGrant|CapabilityLease|GovernInference' -count=1
git add internal/paper/authorization.go internal/paper/authorization_test.go internal/models/model_registry.go internal/policy/service.go
git add -p internal/relay/service.go internal/relay/paper_listener.go internal/relay/service_test.go
git commit -m "feat: add canonical authorization grants"
```

### Task 8: Enforce delegated-authorization attenuation

**Files:**
- Create: `internal/paper/delegation.go`
- Create: `internal/paper/delegation_test.go`
- Modify: `internal/paper/authorization.go`
- Modify: `internal/policy/service.go`
- Modify: `internal/relay/pipeline.go`

**Interfaces:**
- Produces `ValidateDelegation(parent, child *AuthorizationGrant) error` and `IssueChildGrant(...)`.

- [ ] **Step 1: Write table tests**

Cover added models, wider paths, new tools, broader networks, increased budgets, later expiry, changed audience, bad parent digest, exceeded depth, and one valid narrower child.

```go
func TestDelegationRejectsBroadenedTools(t *testing.T) {
    parent := grantWithTools("repo.read")
    child := childOf(parent)
    child.Scope.AllowedTools = []string{"repo.read", "shell.admin"}
    if err := paper.ValidateDelegation(parent, child); err == nil { t.Fatal("broadened child accepted") }
}
```

Define `grantWithTools(tools ...string) *paper.AuthorizationGrant` and `childOf(parent *paper.AuthorizationGrant) *paper.AuthorizationGrant` in the same test file. `childOf` copies the parent, binds `ParentGrantDigest`, reduces expiry by one minute, and decrements delegation depth.

- [ ] **Step 2: Implement attenuation**

Preserve organization/user/session/epoch, require the parent digest, narrow every set, never increase budgets or expiry, decrement depth, and interpret an empty child scope as no authority. A downstream peer must not reuse the parent Harness grant.

- [ ] **Step 3: Test and commit**

```bash
go test ./internal/paper ./internal/policy ./internal/relay -run 'Delegation|Authorization|Tool' -count=1
git add internal/paper/delegation.go internal/paper/delegation_test.go internal/paper/authorization.go internal/policy/service.go internal/relay/pipeline.go
git commit -m "feat: enforce authorization attenuation"
```

### Task 9: Standardize decisions and state freshness

**Files:**
- Create: `internal/paper/decision.go`
- Create: `internal/paper/decision_test.go`
- Create: `internal/paper/state_checkpoint.go`
- Create: `internal/paper/state_checkpoint_test.go`
- Modify: `internal/relay/pipeline.go`
- Modify: `internal/configmgmt/service.go`
- Modify: `internal/policy/service.go`

**Interfaces:**
- Produces `AuthorizationDecision`, `Obligation`, `SignedStateCheckpoint`, `ValidateCheckpointAdvance`, and `ValidateStateFreshness`.

- [ ] **Step 1: Test deny-overrides, pending obligations, expiry, and rollback**

```go
func TestCheckpointRejectsRollback(t *testing.T) {
    if err := paper.ValidateCheckpointAdvance(paper.SignedStateCheckpoint{Sequence: 12}, paper.SignedStateCheckpoint{Sequence: 11}); err == nil { t.Fatal("rollback accepted") }
}
```

- [ ] **Step 2: Implement fixed decisions and obligations**

Use `ALLOW`, `DENY`, and `ALLOW_WITH_OBLIGATIONS`; each obligation has ID, kind, parameters digest, `PENDING|SATISFIED|FAILED`, and evidence reference. Required pending obligations block completion.

- [ ] **Step 3: Implement signed freshness classes and wire them into Relay**

Revocation, issuer keys, epochs, manifests, and endpoints carry monotonic sequence, issued time, expiry, and maximum staleness. Integrity classes fail closed; unexpired signed low-risk read-only state may degrade only when policy allows.

- [ ] **Step 4: Test and commit**

```bash
go test ./internal/paper ./internal/policy ./internal/configmgmt ./internal/relay -run 'Decision|Obligation|Checkpoint|Freshness' -count=1
git add internal/paper/decision.go internal/paper/decision_test.go internal/paper/state_checkpoint.go internal/paper/state_checkpoint_test.go internal/relay/pipeline.go internal/configmgmt/service.go internal/policy/service.go
git commit -m "feat: standardize decisions and state freshness"
```

### Task 10: Finalize deterministic multi-party receipts

**Files:**
- Create: `internal/paper/receipt.go`
- Create: `internal/paper/receipt_test.go`
- Modify: `internal/models/provenance.go`
- Modify: `internal/provenance/service.go`
- Modify: `internal/relay/service.go`
- Modify: `internal/pia/service.go`

**Interfaces:**
- Produces `EvidenceReceiptBody`, `ReceiptAttestation`, `FinalizeReceipt`, `VerifyReceipt`, and selective-disclosure proofs.

- [ ] **Step 1: Test the computed root and final state**

```go
func TestReceiptCommitsComputedRoot(t *testing.T) {
    events := []paper.EvidenceEvent{{Sequence: 1, Digest: []byte("a")}, {Sequence: 2, Digest: []byte("b")}}
    receipt, err := paper.FinalizeReceipt(events, nil); if err != nil { t.Fatal(err) }
    if !bytes.Equal(receipt.ChainRoot, paper.ComputeEvidenceRoot(events)) { t.Fatal("wrong root") }
}
```

- [ ] **Step 2: Implement canonical receipt bodies and scoped attestations**

Commit exchange/final state, sequence bounds, real evidence root, authorization/decision digests, manifest, endpoint, effects, omission manifest, and issue time. Relay, Inference Peer, and Effect Executor attest only to their observations.

- [ ] **Step 3: Correct close ordering**

Transition to `FINALIZING`, append final event, compute root, obtain required attestations, persist, then transition to `COMPLETED`. Evidence failure must not report success. Remove `paper.GenerateID("chainroot")`.

- [ ] **Step 4: Add segmented Merkle inclusion and omission proofs**

Retain the linear chain for order. Test removed disclosed events, modified leaves, and proofs for the wrong root.

- [ ] **Step 5: Test and commit**

```bash
go test ./internal/paper ./internal/provenance ./internal/relay ./internal/pia -run 'Receipt|Evidence|Merkle' -count=1
git add internal/paper/receipt.go internal/paper/receipt_test.go internal/models/provenance.go internal/provenance/service.go internal/relay/service.go internal/pia/service.go
git commit -m "feat: finalize verifiable multi-party receipts"
```

### Task 11: Add transactional effect execution

**Files:**
- Create: `internal/paper/effects.go`
- Create: `internal/paper/effects_test.go`
- Modify: `internal/paper/messages.go`
- Modify: `internal/relay/pipeline.go`
- Modify: `internal/tools/service.go`
- Modify: `internal/replay/service.go`
- Modify: `conformance/conformance_test.go`

**Interfaces:**
- Produces `EffectPrepare`, `EffectAuthorization`, `EffectResult`, and `PREPARED → AUTHORIZED → EXECUTING → COMMITTED|ABORTED`.

- [ ] **Step 1: Write a reconnect test with an atomic fake executor**

Prepare, authorize, execute, reconnect, resend the same operation ID, assert execution count remains one, and return the stored result.

```go
func TestCommittedEffectIsNotRepeatedAfterReconnect(t *testing.T) {
    var calls atomic.Int32
    executor := paper.EffectExecutorFunc(func(context.Context, paper.EffectPrepare) (paper.EffectResult, error) {
        calls.Add(1)
        return paper.EffectResult{ResultDigest: []byte("result")}, nil
    })
    store := paper.NewMemoryEffectStore()
    runCommittedEffect(t, store, executor, "op-1")
    runCommittedEffect(t, store, executor, "op-1")
    if calls.Load() != 1 { t.Fatalf("effect executed %d times", calls.Load()) }
}
```

Define `EffectExecutorFunc`, `MemoryEffectStore`, and `runCommittedEffect` in this task. The production seam accepts a durable store adapter; this test uses the in-memory adapter.

- [ ] **Step 2: Allocate unused tool-range message IDs**

Add `EFFECT_PREPARE`, `EFFECT_AUTHORIZE`, `EFFECT_COMMIT`, `EFFECT_ABORT`, and `EFFECT_STATUS` without renumbering existing IDs.

- [ ] **Step 3: Persist operation ID, nonce, grant digest, input/result digest, executor, state, and retry owner**

Only the retry owner may resume incomplete work. Replace the comment-only `TestInvariant8_NoDuplicateSideEffects` with the real lifecycle test.

- [ ] **Step 4: Test and commit**

```bash
go test ./internal/paper ./internal/tools ./internal/replay ./internal/relay ./conformance -run 'Effect|Duplicate|Invariant8' -count=1
git add internal/paper/effects.go internal/paper/effects_test.go internal/paper/messages.go internal/relay/pipeline.go internal/tools/service.go internal/replay/service.go conformance/conformance_test.go
git commit -m "feat: add transactional effect lifecycle"
```

### Task 12: Define neutral profiles and capability reporting

**Files:**
- Create: `internal/paper/profiles.go`
- Create: `internal/paper/profiles_test.go`
- Create: `registry/extensions.csv`
- Modify: `registry/profiles.csv`
- Modify: `internal/paper/ai_v2.go`
- Modify: `internal/paper/models.go`
- Modify: `adapters/vllm/adapter.go`
- Modify: `adapters/sglang/adapter.go`

**Interfaces:**
- Produces `dari.ai/1`, `dari.tools/1`, `dari.model-supply/1`, `dari.web/1`, `dari.federation/1`, `dari.collab/1`, `dari.media/1`, `NegotiateProfiles(local, remote []ProfileOffer) ([]ProfileResult, error)`, and `EXACT|DEGRADED|UNSUPPORTED`.

- [ ] **Step 1: Test exact agreement, degraded disclosure, and critical-profile rejection**

```go
func TestProfilesRejectUnsupportedCriticalOffer(t *testing.T) {
    _, err := paper.NegotiateProfiles(
        []paper.ProfileOffer{{ID: "dari.ai/1", Critical: true}},
        []paper.ProfileOffer{{ID: "dari.tools/1"}},
    )
    if err == nil { t.Fatal("unsupported critical profile accepted") }
}
```

- [ ] **Step 2: Extract neutral protocol names**

Use Inference Peer, Model Artifact Manifest, Endpoint Authorization, and Governance Relay. Keep PIA/PMP as Patty product adapter names outside the kernel.

- [ ] **Step 3: Register executable profile contracts and provider capability adapters**

Register `dari.ai/1`, `dari.tools/1`, and `dari.model-supply/1` with exact mandatory/optional capability sets, adapter-loss reporting (`EXACT`, `DEGRADED`, or `UNSUPPORTED`), and provider-neutral model/endpoint bindings. Reserve `dari.web/1`, `dari.federation/1`, `dari.collab/1`, and `dari.media/1` for the concrete runtime tasks below; their registry entries must point to executable handlers and conformance manifests rather than schema-only status.

- [ ] **Step 4: Test and commit**

```bash
go test ./internal/paper ./adapters/... -run 'Profile|Capability|Adapter' -count=1
git add internal/paper/profiles.go internal/paper/profiles_test.go registry/extensions.csv registry/profiles.csv internal/paper/ai_v2.go internal/paper/models.go adapters/vllm/adapter.go adapters/sglang/adapter.go
git commit -m "feat: define vendor-neutral protocol profiles"
```

### Task 13: Implement the browser/WebTransport runtime profile

**Files:**
- Create: `internal/webbinding/server.go`
- Create: `internal/webbinding/server_test.go`
- Create: `internal/webbinding/session_store.go`
- Create: `internal/webbinding/session_store_test.go`
- Modify: `internal/relay/paper_listener.go`
- Modify: `internal/relay/server.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/services.go`
- Modify: `internal/paper/transport.go`
- Create: `sdk/web/package.json`
- Create: `sdk/web/src/client.ts`
- Create: `sdk/web/src/client.test.ts`
- Create: `sdk/web/README.md`
- Create: `conformance/testdata/dariweb1/`
- Modify: `deployments/kubernetes/relay.yaml`
- Modify: `deployments/docker/docker-compose.yml`

**Interfaces:**
- Produces `WebBindingServer`, `AcceptWebTransport`, `AcceptWebSocketFallback`, `VerifyWebOrigin`, `VerifyBrowserProof`, `ReconnectSession`, and a browser `DariClient` with `open`, `send`, `close`, and `status` methods.
- WebTransport over HTTP/3 is the primary carrier. The constrained WebSocket fallback carries the same canonical DARI envelope and never introduces cookie, bearer-token, or alternate authorization semantics.

- [ ] **Step 1: Write failing browser-binding tests**

Cover exact origin/site binding, browser-held proof-of-possession, transcript/channel binding, challenge freshness, cross-site rejection, cookie-only rejection, and critical-profile negotiation. The first integration test must prove that a copied bearer token cannot open a governed exchange.

```go
func TestWebBindingRejectsCookieWithoutProofOfPossession(t *testing.T) {
    server := newWebBindingFixture(t)
    _, err := server.Open(webbinding.OpenRequest{Origin: "https://app.example", Cookie: "session-only"})
    if !errors.Is(err, webbinding.ErrMissingProofOfPossession) { t.Fatalf("got %v", err) }
}
```

- [ ] **Step 2: Implement the HTTP/3 and fallback listeners**

Bind the authenticated transcript to the HTTP/3 exporter (or negotiated equivalent), validate `Origin` against organization policy, issue a one-use challenge, and pass the proof to the same peer-authenticator, grant, decision, freshness, effect, and receipt code used by native transports. The WebSocket path must reject an envelope whose profile, transcript digest, or sequence differs.

- [ ] **Step 3: Add durable browser sessions and safe reconnect**

Persist session ID, browser-key thumbprint, origin, last sequence, grant digest, and effect operation IDs in the database/cache boundary. Reconnect requires a fresh proof, replays only missing evidence, and queries `EFFECT_STATUS` instead of re-executing. Add bounded send queues, per-origin rate limits, idle expiry, and metrics for proof failures, reconnect conflicts, and backpressure.

- [ ] **Step 4: Publish the browser SDK and deployment configuration**

Implement `sdk/web/src/client.ts` with WebCrypto key generation/import, challenge signing, canonical DARI framing, status-query reconnect, and explicit `EXACT|DEGRADED|UNSUPPORTED` results. Do not expose raw model endpoints or bypass the Relay. Add HTTP/3 certificates, origin allowlists, proxy headers, and health/readiness checks without putting credentials in manifests.

- [ ] **Step 5: Run profile conformance and commit**

```bash
go test ./internal/webbinding ./internal/relay ./internal/paper -run 'Web|Origin|Browser|Reconnect|WebSocket' -count=1
(cd sdk/web && npm test)
go test ./conformance -run 'DARIWeb|WebTransport|Browser' -count=1
git add internal/webbinding internal/relay/paper_listener.go internal/relay/server.go internal/api/server.go internal/api/services.go internal/paper/transport.go sdk/web conformance/testdata/dariweb1 deployments
git commit -m "feat: implement DARI browser transport profile"
```

### Task 14: Implement bilateral federation and cross-domain verification

**Files:**
- Create: `internal/federation/trust_bundle.go`
- Create: `internal/federation/trust_bundle_test.go`
- Create: `internal/federation/validator.go`
- Create: `internal/federation/validator_test.go`
- Create: `internal/federation/policy_intersection.go`
- Create: `internal/federation/policy_intersection_test.go`
- Modify: `internal/relay/service.go`
- Modify: `internal/relay/pipeline.go`
- Modify: `internal/config/config.go`
- Modify: `internal/sovereign/service.go`
- Create: `conformance/testdata/darifederation1/`
- Modify: `registry/profiles.csv`
- Modify: `registry/crypto.csv`
- Modify: `deployments/kubernetes/relay.yaml`

**Interfaces:**
- Produces `TrustDomain`, `FederationTrustBundle`, `DiscoverTrustBundle`, `ValidateFederatedGrant`, `IntersectPolicy`, `ValidateResidency`, and `VerifyFederatedReceipt`.
- A remote signature is evidence, not local authorization. Every request passes issuer/audience/subject-key validation, local policy, residency, freshness, and receipt-key validation in the receiving domain.

- [ ] **Step 1: Write failing federation tests**

Cover unknown issuer, wrong audience, stale/rolled-back trust bundle, revoked issuer key, policy conflict, narrowed authority, residency violation, missing receipt key, remote-only authorization, and offline cached-bundle expiry.

```go
func TestFederationUsesPolicyIntersection(t *testing.T) {
    local := policy.Allow("repo.read", "kr"); remote := policy.Allow("repo.read", "us")
    if _, err := federation.IntersectPolicy(local, remote); !errors.Is(err, federation.ErrResidencyConflict) { t.Fatalf("got %v", err) }
}
```

- [ ] **Step 2: Implement signed trust-bundle discovery and rollback protection**

Define stable issuer, audience, and trust-domain identifiers. Fetch bundles over configured HTTPS/mTLS endpoints, verify signature and predecessor, persist a monotonic high-water mark, enforce issued/expiry/max-staleness, and fail closed for integrity state. Sovereign deployments import the same bundle format offline and emit an evidence event for import and activation.

- [ ] **Step 3: Implement grant, policy, and receipt federation**

Validate the full parent grant chain, bind the receiving audience and local subject key, intersect every action/resource/time/budget/residency constraint, and require local `ALLOW` plus the remote decision. Verify attestations against the correct domain trust bundle and preserve both domains' provenance roots without rewriting either root.

- [ ] **Step 4: Add federation controls and observability**

Add organization peer allowlists, trust-bundle rotation, emergency domain quarantine, per-domain rate/budget limits, residency configuration, and metrics for stale bundles, rejected issuers, intersection denials, and receipt failures. Health/readiness must distinguish “trust not configured” from “trust stale.”

- [ ] **Step 5: Run profile conformance and commit**

```bash
go test ./internal/federation ./internal/relay ./internal/sovereign -run 'Federat|Trust|Residency|Intersection|Receipt' -count=1
go test ./conformance -run 'Federation|CrossDomain|TrustBundle' -count=1
git add internal/federation internal/relay/service.go internal/relay/pipeline.go internal/config/config.go internal/sovereign/service.go conformance/testdata/darifederation1 registry deployments
git commit -m "feat: implement DARI federation profile"
```

### Task 15: Wire the complete governance hot path into the live inference exchange

**Files:**
- Modify: `internal/relay/paper_listener.go`
- Modify: `internal/relay/pipeline.go`
- Modify: `internal/relay/service.go`
- Modify: `internal/relay/server.go`
- Modify: `internal/paper/ai_v2.go`
- Modify: `internal/catalog/service.go`
- Modify: `internal/policy/service.go`
- Modify: `internal/scheduler/service.go`
- Modify: `internal/detection/service.go`
- Modify: `internal/security/service.go`
- Modify: `internal/context/service.go`
- Modify: `internal/network/service.go`
- Modify: `internal/tools/service.go`
- Modify: `internal/workintel/service.go`
- Modify: `internal/events/service.go`
- Modify: `internal/provenance/service.go`
- Modify: `internal/gpuops/service.go`
- Modify: `internal/replay/service.go`
- Modify: `internal/pia/service.go`
- Modify: `internal/pia/vllm.go`
- Modify: `internal/pia/paper_listener.go`
- Create: `internal/relay/hot_state.go`
- Create: `internal/relay/hot_state_test.go`
- Create: `internal/relay/inference_stream.go`
- Create: `internal/relay/inference_stream_test.go`

**Interfaces:**
- Produces `HotStateCache`, `GovernedInference`, `StreamResult`, and one live `HandleApplicationMessage` path that invokes the 14 stages in order and returns a signed verdict/receipt.
- The hot path reads immutable snapshots from memory or a bounded local cache, never queries the database per token/chunk, and emits usage, event, provenance, and evidence records asynchronously with durable retry semantics.

- [x] **Step 1: Write the end-to-end failing test**

Start an in-process Harness→Relay→PIA exchange with a fake model endpoint. Assert that an unregistered model, revoked Harness, stale policy epoch, DLP violation, capacity denial, unauthorized tool, and unapproved endpoint are rejected before the fake PIA observes the request. Assert that an allowed streaming request produces ordered chunks, usage, provenance, and a final receipt without a manual close call.

- [x] **Step 2: Implement hot-state snapshots and stage enforcement**

Build versioned snapshots for trust bundles, revocations, leases, policy epochs, model catalog, endpoint authorization, capacity, and risk state. Replace permissive helpers in `pipeline.go` with fail-closed lookups that validate the grant/decision/checkpoint chain. Wire the live listener to the pipeline and remove the silent HTTP/OpenAI fallback for protected requests.

- [x] **Step 3: Implement provider-neutral streaming and admission**

Translate `dari.ai/1` requests to vLLM/SGLang through a common stream interface. Enforce catalog-model → signed model artifact → endpoint authorization → PIA identity before dispatch. Connect the fair scheduler, capacity lease, account-integrity detector, tool/network/secret brokers, per-exchange token accounting, cancellation, backpressure, and bounded retries. A provider-specific field that cannot be translated must be reported as `DEGRADED` or rejected.

- [x] **Step 4: Emit live usage, events, provenance, and receipts**

Record normalized usage and latency from actual stream telemetry, append signed events with causal sequence, create provenance spans for prompt/context/model/tool/file/code edges, and call `CloseExchange` automatically on success, denial, cancellation, and transport failure. Evidence failure must be visible to the caller and operator.

- [x] **Step 5: Verify PIA trust and model supply**

Verify the signed Model Artifact Manifest before model load, bind endpoint leases to the verified artifact digest, replace placeholder attestation/key-release values with an explicit software-only or hardware-backed provider result, and enforce re-attestation/revocation on the PIA connection.

Task 15 also owns the currently missing AI semantic contract, real tokenizer/structured-output accounting, GPU telemetry/event-spine wiring, fleet catalog push, inline DLP/injection enforcement, scheduler admission, and the removal of mock inference fallbacks. Each must be observable in the end-to-end trace; a model adapter that returns a plausible response without these records is not a completed path.

- [x] **Step 6: Test the live path and commit**

```bash
go test ./internal/relay ./internal/pia ./internal/paper ./internal/catalog ./internal/scheduler ./internal/detection ./internal/security ./internal/context ./internal/network ./internal/tools ./internal/provenance ./internal/workintel -run 'Pipeline|Inference|Stream|Catalog|Lease|DLP|Scheduler|Usage|Evidence|Provenance|Attestation' -count=1
go test ./test ./... -count=1
git add internal/relay internal/paper/ai_v2.go internal/catalog internal/policy internal/scheduler internal/detection internal/security internal/context internal/network internal/tools internal/workintel internal/events internal/provenance internal/gpuops internal/replay internal/pia
git commit -m "feat: wire DARI governance into live inference"
```

### Task 16: Close the enterprise control-plane, security, and sovereign operations gaps

**Files:**
- Modify: `internal/identity/service.go`
- Modify: `internal/identity/auth.go`
- Modify: `internal/sso/service.go`
- Modify: `internal/models/identity.go`
- Modify: `internal/models/enterprise.go`
- Modify: `internal/policy/service.go`
- Modify: `internal/models/policy.go`
- Modify: `internal/configmgmt/service.go`
- Modify: `internal/compliance/service.go`
- Modify: `internal/privacy/service.go`
- Modify: `internal/keymgmt/service.go`
- Modify: `internal/events/service.go`
- Modify: `internal/reporting/service.go`
- Modify: `internal/sovereign/service.go`
- Modify: `internal/attestation/service.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/services.go`
- Create: `internal/retention/worker.go`
- Create: `internal/retention/worker_test.go`
- Create: `internal/onboarding/service.go`
- Create: `internal/onboarding/service_test.go`
- Create: `internal/observability/metrics.go`
- Create: `internal/observability/metrics_test.go`
- Modify: `deployments/kubernetes/control-plane.yaml`
- Modify: `deployments/kubernetes/relay.yaml`
- Modify: `deployments/docker/docker-compose.yml`

**Interfaces:**
- Produces `OrgHierarchyResolver`, `AdminAuthorization`, `SignedSSOProvider`, `SCIMProvisioner`, `RetentionWorker`, `KMSProvider`, `ComplianceAssessment`, `RolloutCoordinator`, and `EnterpriseMetrics`.
- Administrative reads and writes use a server-side privacy-aware authorization boundary regardless of which console rendered the request. Platform operators cannot infer prompt, billing, communications, or work-intelligence content without an explicit scoped grant and audit event.

- [x] **Step 1: Test identity, hierarchy, and admin isolation**

Add failing tests for SAML signature rejection, OIDC state/nonce validation, SCIM create/update/deactivate, group→affiliate→division resolution, delegated-admin scope, contractor expiry, Harness quarantine, and the two-console privacy boundary. Exercise HTTP authorization middleware rather than only service methods.

- [x] **Step 2: Implement real policy/compliance records and approvals**

Persist policy packs, ABAC attributes, epoch diffs, exception approvals, acknowledgement campaigns, compliance controls, evidence attachments, remediation tasks, CSAP/ISMS-P overlays, and government/sovereign applicability. Replace static template scoring and “assessment pending” strings with evidence-backed evaluation that cites source, version, and evidence digest for every control.

- [x] **Step 3: Implement key, attestation, retention, and offline operations**

Define a `KMSProvider` interface with local-development, customer-KMS, and HSM-backed implementations. Rotate/revoke signing keys with signed checkpoints, never persist plaintext private keys in ordinary models, and make trust-bundle/model/update imports verify signatures and rollback state. Run retention/purge workers that honor legal holds, emit deletion evidence, and preserve redaction manifests. Make sovereign offline updates and time proofs real state transitions rather than “would apply” placeholders.

- [x] **Step 4: Implement onboarding, rollout, migration, and HA controls**

Provide an idempotent organization/onboarding flow, import existing v1 data into DARI compatibility records, stage policy/catalog changes through validation→approval→rollout→rollback, and expose readiness for PostgreSQL, durable event storage, cache, object storage, and multi-replica Relay deployment. Health checks fail closed when required trust/policy/catalog state is unavailable.

- [x] **Step 5: Add metrics, reports, audit, and operator evidence**

Instrument request admission, policy denials, queue time, TTFT, streaming throughput, GPU/model health, usage, security findings, evidence finalization, federation, WebTransport, and retention jobs. Generate signed scheduled governance/usage/security/executive reports and export machine-verifiable bundles without leaking secrets. Every admin action and configuration transition enters the durable event spine.

Task 16 also closes the explicit control-plane gaps for scalable global search, account billing/payment and chargeback, wallboard/kiosk and historical comparison, two-console privacy enforcement, onboarding/migration, scheduled reporting, and work-intelligence dispute/bias/gaming controls. The Korean enterprise differentiators (group/affiliate and SI modes, shadow-AI inventory, change board, sensitivity heatmap, policy acknowledgement, skills matrix, exception marketplace, model recall, forced versions/rings, architecture packs, executive brief, freeze, offboarding/evidence handoff, and ROI comparison) are product workflows with persisted state and tests, not future-facing copy.

- [x] **Step 6: Test and commit**

```bash
go test ./internal/identity ./internal/sso ./internal/policy ./internal/compliance ./internal/privacy ./internal/keymgmt ./internal/events ./internal/reporting ./internal/sovereign ./internal/attestation ./internal/retention ./internal/onboarding ./internal/observability ./internal/api -run 'SSO|OIDC|SCIM|Hierarchy|Admin|Policy|Compliance|Retention|LegalHold|KMS|Attestation|Offline|Onboard|Rollout|Metric' -count=1
go test ./test ./... -count=1
git add internal/identity internal/sso internal/models internal/policy internal/configmgmt internal/compliance internal/privacy internal/keymgmt internal/events internal/reporting internal/sovereign internal/attestation internal/retention internal/onboarding internal/observability internal/api deployments
git commit -m "feat: complete enterprise control-plane operations"
```

### Task 17: Make tools, SCM, sandbox, and provenance executable enterprise boundaries

**Files:**
- Modify: `internal/tools/service.go`
- Modify: `internal/mcp/service.go`
- Modify: `internal/mcpmarket/service.go`
- Modify: `internal/network/service.go`
- Modify: `internal/secret/service.go`
- Modify: `internal/gitscm/service.go`
- Modify: `internal/sandbox/service.go`
- Modify: `internal/connectors/service.go`
- Modify: `internal/provenance/service.go`
- Modify: `internal/models/tool_runtime.go`
- Modify: `internal/models/project.go`
- Create: `internal/tools/executor.go`
- Create: `internal/tools/executor_test.go`
- Create: `internal/sandbox/runtime.go`
- Create: `internal/sandbox/runtime_test.go`
- Create: `internal/gitscm/provider.go`
- Create: `internal/gitscm/provider_test.go`

**Interfaces:**
- Produces `ToolExecutor`, `NetworkBroker`, `SecretBroker`, `RepositoryProvider`, `SandboxRuntime`, `ConnectorDispatcher`, and `ProvenanceRecorder` implementations that accept only a verified DARI grant/decision and return evidence references.

- [x] **Step 1: Test tool/MCP/network/secret denial before execution**

Use fake executors and assert that an unauthorized command, MCP server, destination, or secret request is rejected before the external process/socket is touched. Verify approval obligations, tool descriptors, network byte accounting, secret lease expiry, kill switches, and transactional effect receipts.

- [x] **Step 2: Implement real tool and connector execution**

Resolve registered MCP/tools through signed descriptors, enforce input/output schemas, run high-risk actions through the effect lifecycle, and dispatch Slack/Teams/Kakao Work/Jira/CI connectors only through stored scoped credentials. Normalize provider-specific results and record retries/failures in the event spine.

- [x] **Step 3: Implement SCM and change-set binding**

Add GitHub/GitLab/local provider adapters for clone/fetch/webhook, branch protection, baseline tagging, diff/commit binding, and repository sensitivity. Bind every accepted file/code change to session, exchange, model, tool, policy, and commit digests; reject writes to frozen or unapproved branches.

- [x] **Step 4: Implement isolated sandbox execution**

Run commands in an actual isolated runtime selected by policy (container/VM/remote worker), enforce CPU/memory/network/filesystem limits, stream output with backpressure, capture forensic snapshots, and reconcile crash/retry state through `EFFECT_STATUS`. A recorded sandbox definition alone is not evidence of execution.

- [x] **Step 5: Feed live provenance and test the boundaries**

Create an end-to-end test that performs a governed tool call, network request, secret lease, sandbox command, repository diff, and commit candidate, then verifies the complete provenance graph and receipt. Include changed-input, replay, timeout, connector outage, branch-policy, and crash cases.

- [x] **Step 6: Test and commit**

```bash
go test ./internal/tools ./internal/mcp ./internal/mcpmarket ./internal/network ./internal/secret ./internal/gitscm ./internal/sandbox ./internal/connectors ./internal/provenance -run 'Tool|MCP|Network|Secret|Git|SCM|Sandbox|Connector|Provenance|Effect' -count=1
go test ./... -count=1
git add internal/tools internal/mcp internal/mcpmarket internal/network internal/secret internal/gitscm internal/sandbox internal/connectors internal/provenance internal/models
git commit -m "feat: enforce governed tools SCM and sandbox boundaries"
```

### Task 18: Deliver governed collaboration, media, and compilable SDKs

**Files:**
- Create: `internal/paper/collaboration.go`
- Create: `internal/paper/collaboration_test.go`
- Create: `internal/paper/media.go`
- Create: `internal/paper/media_test.go`
- Modify: `internal/paper/messages.go`
- Modify: `internal/communications/service.go`
- Modify: `internal/realtime/service.go`
- Modify: `internal/models/communications.go`
- Modify: `internal/provenance/service.go`
- Rename: `sdk/piapi/piapi.go.txt` → `sdk/piapi/piapi.go`
- Rename: `sdk/examples/simple-pia.go.txt` → `sdk/examples/simple-pia.go`
- Create: `sdk/dari/dari.go`
- Create: `sdk/dari/dari_test.go`
- Modify: `web/src/pages/Communications.tsx`
- Modify: `web/src/pages/Sessions.tsx`
- Modify: `web/src/pages/LiveView.tsx`
- Create: `conformance/testdata/daricollab1/`
- Create: `conformance/testdata/darimedia1/`
- Modify: `registry/profiles.csv`

**Interfaces:**
- Produces `CollaborationStream`, `PresenceUpdate`, `BroadcastDelivery`, `FileTransfer`, `VoiceMessage`, and importable Go/TypeScript DARI clients. `dari.collab/1` and `dari.media/1` reuse grant, decision, freshness, receipt, and effect semantics; they are not side channels.

- [x] **Step 1: Test ordered encrypted delivery**

Write tests for per-conversation authorization, presence expiry, targeted broadcasts, direct/group message encryption using a standard MLS/AEAD adapter, ordered delivery, reconnect replay, and unauthorized administrative reads. The Harness must receive the same event and evidence semantics as the web client.

Task 18 also owns the E2E encrypted voice/media path, resumable file scanning/retention/legal-hold behavior, compilable SDKs (including replacing `.go.txt` examples), and live UI streams for communications, sessions, and wallboard/history views. A generated client or scripted page without a real DARI session and receipt is not an implementation.

- [x] **Step 2: Implement chat, presence, broadcast, and voice-message streams**

Carry content class, sender/conversation, sequence, classification, retention, and content digest in canonical messages. Keep asynchronous voice messages distinct from any live-media extension; when `dari.media/1` is negotiated, stream Opus/WebRTC-compatible chunks with explicit authorization, cancellation, usage, and receipt events.

- [x] **Step 3: Implement governed file transfer**

Implement `OFFER → METADATA_POLICY → CONTENT_POLICY → RECIPIENT_AUTHORIZATION → ACCEPT → CHUNK_TRANSFER → VERIFY → STORE/DELIVER → RECEIPT`, with resumable ranges, per-chunk digests, malware/PII scanning hooks, storage encryption, retention/legal hold, and no delivery after a failed policy decision.

- [x] **Step 4: Turn SDK examples into tested libraries and wire the UI**

Replace `.go.txt` examples with compilable packages that open DARI sessions, negotiate profiles, send governed inference/collaboration messages, reconnect safely, and verify receipts. Update the web console to consume live streams, show policy/evidence state, and deep-link to sessions, exchanges, files, and provenance instead of rendering scripted data.

- [x] **Step 5: Test and commit**

```bash
go test ./internal/paper ./internal/communications ./internal/realtime ./internal/provenance ./sdk/... -run 'Collab|Media|Presence|Broadcast|File|Voice|Receipt|SDK' -count=1
(cd sdk/web && npm test)
go test ./conformance -run 'Collab|Media|File|SDK' -count=1
git add internal/paper internal/communications internal/realtime internal/provenance internal/models sdk web/src/pages/Communications.tsx web/src/pages/Sessions.tsx web/src/pages/LiveView.tsx conformance/testdata/daricollab1 conformance/testdata/darimedia1 registry/profiles.csv
git commit -m "feat: deliver governed collaboration and SDKs"
```

### Task 19: Replace proxy conformance tests with black-box invariants

**Files:**
- Modify: `conformance/conformance_test.go`
- Modify: `conformance/README.md`
- Create: `conformance/runner_test.go`
- Create: `conformance/manifest.json`
- Create: `conformance/testdata/dari1/`
- Create: `conformance/testdata/dariweb1/`
- Create: `conformance/testdata/darifederation1/`
- Create: `conformance/testdata/daricollab1/`
- Create: `conformance/testdata/darimedia1/`

**Interfaces:**
- Produces capability-scoped results and canonical/negative vectors.

- [x] **Step 1: Make invariants 1–12 call real seams**

Remove comment-only tests and tests substituting credential time validity for authorization. Exercise verifier, Relay authorization, profile gate, effect lifecycle, and receipt verification.

- [x] **Step 2: Add context-authorization and negative vectors**

Attempt excluded context and prove denial before the fake Inference Peer receives it. Add bad signature, broadened child, stale state, invalid critical field, modified receipt, and duplicate effect vectors.

- [x] **Step 3: Publish exact support in `manifest.json`**

Each record names profile, capability, test, normative requirement, deployment mode, and result. Do not report any profile runtime support without runtime tests, deployment evidence, and a capability manifest. Web, federation, collaboration, and media profiles are executable tracks; `UNSUPPORTED` is valid only when a deployment explicitly disables a tested capability and the manifest records the reason. The runner must execute against at least the root implementation and one independent SDK/client implementation, compare canonical bytes and state transitions, and fail if a negative case reaches the external executor.

- [x] **Step 4: Test and commit**

```bash
go test ./conformance -count=1
go test ./... -count=1
git add conformance
git commit -m "test: enforce protocol invariants end to end"
```

---

## Phase 3 — PAPER to DARI Rename

**Phase entry gate:** Before any `git mv`, every rename target must be free of uncommitted, non-task-owned changes in its owning repository. Compare each path against the `/tmp/dari-task-*-before.patch` snapshots. Commit or preserve task-owned work with patch staging; if unrelated user changes remain inseparable, stop the rename phase and report the exact paths instead of staging them through a move.

### Task 20: Rename the Go package with a bounded compatibility adapter

**Files:**
- Rename: `internal/paper/` → `internal/dari/`
- Rename: `internal/relay/paper_listener.go` → `internal/relay/dari_listener.go`
- Rename: `internal/relay/paper_listener_test.go` → `internal/relay/dari_listener_test.go`
- Rename: `internal/relay/paper_client.go` → `internal/relay/dari_client.go`
- Rename: `internal/pia/paper_listener.go` → `internal/pia/dari_listener.go`
- Create: `internal/dari/legacy_paper1.go`
- Modify: all Go files returned by `rg -l 'internal/paper|\bpaper\.' --glob '*.go'`.

**Interfaces:**
- Produces `dari.ALPNProtocol == "dari/1"`, `dari.LegacyALPNProtocol == "paper/1"`, `relay.SupportedALPNs() []string`, and unchanged message IDs.

- [x] **Step 1: Test ALPN ordering**

```go
func TestDARIListenerALPNs(t *testing.T) {
    if got := relay.SupportedALPNs(); !slices.Equal(got, []string{"dari/1", "paper/1"}) { t.Fatalf("unexpected ALPNs: %v", got) }
}
```

- [x] **Step 2: Move with `git mv` and mechanically update package declarations, imports, selectors, logs, and exported comments**

Before moving, save the exact dependent-file inventory with `rg -l 'internal/paper|\bpaper\.' --glob '*.go' > /tmp/dari-root-dependent-go-files.txt`. Rename `PaperALPN`, `PaperListener`, and `NewPaperListener` to DARI equivalents. Do not alter message numbers, CBOR labels, persisted columns, or legacy fixture bytes.

- [x] **Step 3: Isolate the legacy literal**

```go
package dari

const LegacyALPNProtocol = "paper/1"
```

- [x] **Step 4: Test and commit**

```bash
go test ./internal/dari ./internal/relay ./internal/pia ./conformance -count=1
go test ./... -count=1
git status --short
git add -p -- internal/dari internal/relay internal/pia
while IFS= read -r file; do
    [ -f "$file" ] && git add -p -- "$file"
done < /tmp/dari-root-dependent-go-files.txt
git diff --cached --name-only -- '*.go' >> /tmp/dari-owned-go-files.txt
git diff --cached --check
git commit -m "refactor: rename protocol package from PAPER to DARI"
```

Before `git add -p`, confirm the already-staged `git mv` entries and stage only the moved paths plus exact dependent Go files shown by `git status --short`. Do not stage unrelated pre-existing hunks.

### Task 21: Rename Patty Code's protocol client

**Files:**
- Rename: `patty-code-pccp/internal/paperproto/` → `patty-code-pccp/internal/dariproto/`
- Rename: `patty-code-pccp/internal/provider/paper/` → `patty-code-pccp/internal/provider/dari/`
- Create: `patty-code-pccp/internal/dariproto/legacy_paper1.go`
- Create: `patty-code-pccp/internal/dariproto/compatibility_test.go`
- Modify: `patty-code-pccp/cmd/patcode/main.go`
- Modify: `patty-code-pccp/internal/config/config.go`
- Modify: `patty-code-pccp/internal/provider/provider.go`

**Interfaces:**
- Produces a Patty Code DARI client preferring `dari/1` with explicit legacy compatibility.

- [ ] **Step 1: Record nested-repository status and baseline tests**

```bash
git -C patty-code-pccp status --short
(cd patty-code-pccp && go test ./internal/paperproto/... -count=1)
```

- [ ] **Step 2: Move the package in its owning repository and update imports/selectors**

New clients offer `dari/1` first. They load the enrolled Peer Credential and private proof-of-possession key from the configured secure provider, bind each proof to the negotiated transcript/challenge/profile, and accept `paper/1` only when compatibility is enabled. They never drop authorization or receipt requirements.

Keep the legacy ALPN literal and its byte-level compatibility assertion only in `legacy_paper1.go` and `compatibility_test.go`; active client paths refer to the named legacy constant.

- [ ] **Step 3: Test and commit in the owning repository**

```bash
git -C patty-code-pccp add internal/dariproto internal/provider/dari internal/provider/provider.go internal/config/config.go cmd/patcode/main.go
git -C patty-code-pccp diff --cached --name-only -- '*.go' >> /tmp/dari-patty-code-owned-go-files.txt
(cd patty-code-pccp && while IFS= read -r file; do [ -n "$file" ] && [ -f "$file" ] && gofmt -w "$file"; done < <(sort -u /tmp/dari-patty-code-owned-go-files.txt))
(cd patty-code-pccp && go test ./internal/dariproto/... -count=1)
(cd patty-code-pccp && go test ./internal/provider/dari ./internal/provider/... ./cmd/patcode -count=1)
git -C patty-code-pccp commit -m "refactor: rename protocol client from PAPER to DARI"
```

`patty-code-pccp/.git` is a gitdir file, so this task is committed in the nested repository and never staged in the PCCP root repository.

### Task 22: Rename every active document, artifact, registry entry, comment, and UI label

**Files:**
- Rename: `docs/plans/PAPER/` → `docs/plans/DARI/`
- Rename: `PAPER.md` → `DARI.md`
- Rename: the three `PAPER_*v1.0.md` files to `DARI_*v1.0.md`.
- Rename: arXiv PDFs and source archive to `DARI_arXiv.pdf`, `DARI_arXiv_KO.pdf`, and `DARI-arXiv-source.tar.gz`.
- Modify: the complete literal inventory, currently 114 text files.

**Interfaces:**
- Produces DARI-only active naming with legacy terminology confined to compatibility/migration evidence.

- [ ] **Step 1: Capture a fresh inventory**

```bash
rg -l --hidden --glob '!.git/**' --glob '!**/node_modules/**' --glob '!**/*.pdf' '(PAPER|Paper|paper/1|paper\.[a-z]|internal/paper|paperproto)' . > /tmp/dari-rename-before.txt
```

- [ ] **Step 2: Rename paths with `git mv` and update authoritative sources first**

Update specs, plans, READMEs, comments, exported docs, errors/logs, UI copy, SDK examples, registries, LaTeX, Makefile targets, and archive scripts. Keep historical narrative only in `docs/plans/DARI/PAPER_TO_DARI_MIGRATION.md`; keep the minimum normative `paper/1` references required to specify and test the bounded compatibility profile in the explicit allowlist below.

- [ ] **Step 3: Rebuild DARI artifacts**

```bash
make -C docs/plans/DARI/arxiv clean
make -C docs/plans/DARI/arxiv verify
make -C docs/plans/DARI/arxiv archive
```

- [ ] **Step 4: Enforce the legacy allowlist**

```bash
rg -n --hidden --glob '!.git/**' --glob '!**/node_modules/**' --glob '!**/*.pdf' '(PAPER|Paper|paper/1|paper\.[a-z]|internal/paper|paperproto)' . > /tmp/dari-legacy-matches.txt
cut -d: -f1 /tmp/dari-legacy-matches.txt | sort -u
```

Every match must be compatibility semantics or historical attribution in one of these bounded locations:

```text
internal/dari/legacy_paper1.go
internal/dari/compatibility_test.go
conformance/testdata/paper1/
patty-code-pccp/internal/dariproto/legacy_paper1.go
patty-code-pccp/internal/dariproto/compatibility_test.go
docs/plans/DARI/DARI_Protocol_Specification_v1.0.md (compatibility section only)
docs/plans/DARI/DARI_COMPATIBILITY_AND_PROFILE_MAP.md (compatibility mapping only)
docs/plans/DARI/PAPER_TO_DARI_MIGRATION.md
docs/superpowers/plans/2026-08-14-dari-protocol-evolution-implementation.md (historical task and compatibility references only)
```

Any match outside those paths fails the gate. Within the two normative documents and this implementation plan, inspect every matching line and reject active names, examples, or identifiers that are not required to explain compatibility or the completed migration history.

- [ ] **Step 5: Commit exact rename paths**

```bash
git status --short
# Patch-stage only task-owned modified paths from /tmp/dari-rename-before.txt;
# git mv entries are already staged. Add regenerated DARI publication artifacts explicitly.
git add docs/plans/DARI/arxiv/DARI_arXiv.pdf docs/plans/DARI/arxiv/DARI_arXiv_KO.pdf docs/plans/DARI/arxiv/DARI-arXiv-source.tar.gz
git diff --cached --check
git commit -m "docs: complete repository-wide DARI naming migration"
```

### Task 23: Add open-spec governance and durable Patty attribution

**Files:**
- Create: `NOTICE`
- Create: `AUTHORS.md`
- Create: `GOVERNANCE.md`
- Create: `CONTRIBUTING.md`
- Create: `SECURITY.md`
- Create: `CITATION.cff`
- Create after approval: the exact code/spec license files selected at the decision gate (`LICENSE`, `LICENSES/`, or a specification-local license as applicable)
- Modify: `DARI.md`
- Modify: `docs/plans/DARI/DARI_Protocol_Specification_v1.0.md`

**Interfaces:**
- Consumes: completed DARI naming and Patty Co., Ltd. attribution.
- Produces: an independently implementable open-spec repository whose origin, stewardship, contribution process, security contact, citation, and conformance-language rules are explicit.

- [ ] **Step 1: Stop at the code/spec license decision gate**

Do not invent or infer a license. Present the user/legal reviewer with separate choices for implementation code and specification text, including patent implications and whether the “DARI Compatible” name needs a trademark policy. Record the approved exact SPDX identifiers, license-file layout, and specification license before creating license files or changing file headers.

- [ ] **Step 2: Add factual origin and citation metadata**

`NOTICE`, `AUTHORS.md`, and `CITATION.cff` state that DARI was originated by Patty Co., Ltd. They must not imply that every later contributor is a Patty employee or that independent implementations are Patty products.

- [ ] **Step 3: Define contribution and governance mechanics**

`GOVERNANCE.md` defines the initial Patty stewardship period, public proposal/review process, registry allocation process, versioning authority, security-errata process, and a path to broader maintainership. `CONTRIBUTING.md` links schema, vector, conformance, and compatibility requirements. `SECURITY.md` provides the disclosure process without embedding credentials or private addresses from environment files.

- [ ] **Step 4: Define fair compatibility language**

Permit independent projects to describe themselves as “DARI compatible” only for profiles present in their machine-readable conformance manifest. Reserve wording that implies Patty certification or endorsement unless Patty has actually issued it.

- [ ] **Step 5: Verify attribution and license consistency**

```bash
rg -n 'Patty Co\., Ltd\.|DARI compatible|conformance|steward' NOTICE AUTHORS.md GOVERNANCE.md CONTRIBUTING.md SECURITY.md CITATION.cff DARI.md
rg -n 'Patty Research' NOTICE AUTHORS.md GOVERNANCE.md CONTRIBUTING.md SECURITY.md CITATION.cff DARI.md docs/plans/DARI/DARI_Protocol_Specification_v1.0.md
git diff --check -- NOTICE AUTHORS.md GOVERNANCE.md CONTRIBUTING.md SECURITY.md CITATION.cff DARI.md docs/plans/DARI/DARI_Protocol_Specification_v1.0.md
```

Expected: required attribution/governance terms are present and `Patty Research` has no matches.

- [ ] **Step 6: Commit after the license decision has been recorded**

```bash
git add NOTICE AUTHORS.md GOVERNANCE.md CONTRIBUTING.md SECURITY.md CITATION.cff DARI.md docs/plans/DARI/DARI_Protocol_Specification_v1.0.md
for path in LICENSE LICENSES docs/plans/DARI/LICENSE; do
    [ -e "$path" ] && git add "$path"
done
test -e LICENSE || test -d LICENSES || test -e docs/plans/DARI/LICENSE
git commit -m "docs: publish DARI open-spec governance and attribution"
```

### Task 24: Final release gate

**Files:**
- Modify only files proven defective by these checks.

**Interfaces:**
- Produces release-ready DARI source, PDFs, and bounded PAPER v1 compatibility.

The release gate is two-repository: a clean root checkout is insufficient while `patty-code-pccp` has uncommitted source or placeholder authentication. Gate D cannot pass until both repositories have owning-repository commits and the nested status is empty.

- [ ] **Step 1: Format and test root source**

```bash
while IFS= read -r file; do
    [ -n "$file" ] && [ -f "$file" ] && gofmt -w "$file"
done < <(sort -u /tmp/dari-owned-go-files.txt)
go test ./... -count=1
go vet ./...
```

- [ ] **Step 2: Test Patty Code from the correct module root**

```bash
(cd patty-code-pccp && go test ./... -count=1)
```

- [ ] **Step 3: Run security and compatibility gates**

```bash
go test ./conformance -count=1
go test ./internal/dari ./internal/relay ./internal/policy ./internal/provenance ./internal/webbinding ./internal/federation ./internal/communications ./internal/realtime ./internal/gitscm ./internal/sandbox ./internal/tools -run 'Auth|Grant|Delegation|Decision|Checkpoint|Receipt|Evidence|Effect|Compatibility|Web|Federat|Collab|Media|SCM|Sandbox|Tool' -count=1
go test ./... -count=1
```

- [ ] **Step 4: Reconcile claims and rebuild the papers**

Update `docs/plans/DARI/arxiv/implementation-claim-matrix.tsv` only from passing runtime/conformance evidence: move C3–C8 and the collaboration/media rows to `implemented` only when their named gates pass, record exact test/manifest identifiers, and keep primitive benchmark values unchanged. Remove internal source file paths from publication prose; describe evidence by subsystem and public artifact instead.

```bash
rg -n 'implementation-required|extension-profile|schema-only|prototype|MVP|Patty Research|internal/paper|paperproto' docs/plans/DARI/arxiv/main.tex docs/plans/DARI/arxiv/main_ko.tex
make -C docs/plans/DARI/arxiv clean
make -C docs/plans/DARI/arxiv verify
pdfinfo docs/plans/DARI/arxiv/DARI_arXiv.pdf
pdfinfo docs/plans/DARI/arxiv/DARI_arXiv_KO.pdf
```

- [ ] **Step 5: Check literals, diff integrity, and protected files**

```bash
rg -n --hidden --glob '!.git/**' --glob '!**/node_modules/**' --glob '!**/*.pdf' '(PAPER|Paper|paper/1|paper\.[a-z]|internal/paper|paperproto)' . > /tmp/dari-legacy-matches-final.txt
cut -d: -f1 /tmp/dari-legacy-matches-final.txt | sort -u
git status --short
git -C patty-code-pccp status --short
git diff --check
git diff --stat
```

Confirm only the bounded compatibility/lineage locations from Task 22 match, every normative-document match is compatibility-only, no protected environment/secrets file changed, no unrelated user work is staged, and no LaTeX temporary file was added.

- [ ] **Step 6: Commit verification fixes only when checks required source changes**

```bash
git commit -m "chore: verify DARI migration and compatibility"
```

Skip the commit if verification produced no source change.

---

## Release Gates

### Gate A — Paper

- English and Korean PDFs build without undefined references.
- The claim matrix distinguishes implemented, implementation-required, and runtime-gated claims during development, then records every promoted claim with a passing runtime/conformance manifest before release.
- DARI terminology and Patty Co., Ltd. attribution are consistent.
- Existing benchmark values remain unchanged unless raw benchmarks are rerun and retained.

### Gate B — Protocol

- Live peer credentials and transcript proofs are verified.
- Authorization scope is completely canonicalized and signed.
- Child grants only attenuate authority.
- Policy decisions and state freshness are enforced.
- Receipts contain the computed root and required attestations.
- Reconnect cannot duplicate committed effects.
- Conformance tests exercise real enforcement seams.

### Gate C — Enterprise runtime

- WebTransport/HTTP/3 and constrained WebSocket clients prove origin binding, browser proof-of-possession, safe reconnect, and effect-status behavior.
- Federation proves bilateral trust-bundle freshness, issuer/audience validation, policy intersection, residency enforcement, and cross-domain receipt verification.
- Every protected inference request traverses the live 14-stage Relay pipeline; no direct OpenAI-compatible fallback can bypass it.
- Catalog → signed model artifact → endpoint authorization → PIA identity is enforced before model dispatch, and real stream telemetry feeds metering, events, provenance, and receipts.
- SSO/SCIM, delegated administration, privacy-aware console authorization, KMS/HSM seams, retention/legal hold, compliance evidence, onboarding/migration, rollout/rollback, and sovereign offline operations are executable and tested.
- Tools, MCP, network, secrets, SCM, sandbox, collaboration, media, file transfer, and SDKs return verifiable evidence and cannot create an ungoverned side channel.
- `manifest.json` reports exact capabilities and deployment evidence for every advertised profile; schema presence alone never qualifies.

### Gate D — Rename

- Active packages, symbols, comments, docs, UI, registries, SDK examples, LaTeX, PDFs, and archives use DARI.
- `paper/1` is accepted only by the compatibility adapter.
- Numeric message IDs and golden legacy vectors remain stable.
- Root and nested-repository tests pass.
- Environment and secrets files remain untouched.
