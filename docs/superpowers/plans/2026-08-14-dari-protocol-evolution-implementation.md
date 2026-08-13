# DARI Protocol Evolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the actively developed PAPER implementation into the DARI open protocol by updating the English and Korean arXiv paper first, completing the protocol's security-critical behavior second, and performing a compatibility-preserving repository-wide rename last.

**Architecture:** Reuse the existing framing, deterministic CBOR, COSE, TLS/TCP, QUIC, connection state, Relay, inference adapters, and registries. Deepen four kernel objects—Peer Credential, Authorization Grant, Governed Exchange, and Evidence Receipt—behind neutral interfaces. Preserve `paper/1` only in a legacy adapter while all active packages, comments, documentation, UI copy, and publication artifacts move to DARI.

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
- Generated LaTeX outputs are rebuilt, never hand-edited.
- Every behavior change starts with a failing test and ends with targeted tests plus `go test ./...`.

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

- [ ] **Step 1: Create the claim matrix**

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

- [ ] **Step 2: Rewrite the title and kernel description**

Use `\title{DARI: Delegated Authorization and Receipts for Governed AI Inference}`. Define Credential, Authorization Grant, Governed Exchange, and Evidence Receipt as the kernel. Move PIA/PMP into the Patty reference profile and use Inference Peer/Model Artifact Manifest in the neutral core.

- [ ] **Step 3: Add the compatibility lineage without co-branding**

Use: “DARI evolved from Patty Co., Ltd.'s earlier internal protocol implementation. The evaluated compatibility profile preserves its deployed record and message semantics while the open specification generalizes authorization and receipt interfaces.”

- [ ] **Step 4: Add the attenuation equation**

```latex
G_i = \operatorname{Sign}_{K_i}(\mathrm{iss},\mathrm{sub},\mathrm{aud},\mathrm{cnf},\mathrm{scope}_i,t_{nbf},t_{exp},H(G_{i-1})),
\qquad \mathrm{scope}_i \subseteq \mathrm{scope}_{i-1}.
```

Retain existing primitive benchmarks as primitive measurements; do not portray them as delegation, browser, or federation measurements.

- [ ] **Step 5: Preserve the evidence package and regenerate stale conceptual graphics**

Retain the existing four figures, three empirical/comparison tables, equations, B0--B4 baseline design, and checked-in benchmark dataset. Regenerate the two raster conceptual figures with the previously selected Qwen image model so they use DARI and the neutral kernel vocabulary; do not hand-draw replacements. The overview currently contains embedded `PAPER PATH` and `PAPER RELAY` labels, while the lifecycle graphic still embeds lease/PMP-era terms. Treat both as stale publication artifacts. Generated figures remain conceptual and must not depict unmeasured performance or implementation coverage. If regeneration would incur new paid API usage beyond the already approved graphics work, obtain explicit execution-time consent before invoking it.

- [ ] **Step 6: Verify terminology and commit**

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

- [ ] **Step 1: Use the Korean title and definition**

```latex
\title{DARI: 거버넌스 기반 AI 추론을 위한 위임 인가와 검증 가능 실행 증거}
```

Define: `위임 인가(delegated authorization)는 권한 주체가 보유한 권한의 일부를 명시적인 범위와 조건에 따라 다른 참여자에게 부여하는 검증 가능한 관계를 의미한다. 이하에서는 이를 권한 위임이라 한다.`

- [ ] **Step 2: Apply this terminology consistently**

```text
Authorization Grant = 인가 증서
Governed Exchange = 거버넌스 적용 교환
Evidence Receipt = 검증 가능 실행 증거
Inference Peer = 추론 피어
Model Artifact Manifest = 모델 아티팩트 명세서
Policy Epoch = 정책 에포크
```

- [ ] **Step 3: Verify parity and commit**

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

- [ ] **Step 1: Add a strict verification target**

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

- [ ] **Step 2: Build and render every page**

```bash
make -C docs/plans/PAPER/arxiv clean
make -C docs/plans/PAPER/arxiv verify
make -C docs/plans/PAPER/arxiv archive
mkdir -p /tmp/dari-paper-en /tmp/dari-paper-ko
pdftoppm -png -r 120 docs/plans/PAPER/arxiv/PAPER_arXiv.pdf /tmp/dari-paper-en/page
pdftoppm -png -r 120 docs/plans/PAPER/arxiv/PAPER_arXiv_KO.pdf /tmp/dari-paper-ko/page
```

Inspect every figure, equation, table, reference page, and appendix for clipping, missing glyphs, arrow overlap, overflow, and untranslated Korean body prose.

- [ ] **Step 3: Commit**

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
- Produces: normative schemas, invariants, compatibility rules, and profile boundaries consumed by Tasks 5–13.

- [ ] **Step 1: Define the neutral kernel and Patty reference profile**

Specify Peer Credential, Authorization Grant, Governed Exchange, Authorization Decision, and Evidence Receipt as neutral objects. Define Governance Relay, Inference Peer, Effect Executor, and Evidence Verifier as roles. Keep Harness, PIA, PMP, Patty Code, vLLM, and SGLang in a named Patty reference profile rather than the core.

- [ ] **Step 2: Add complete normative schemas and validation algorithms**

Add deterministic CDDL for Authorization Grant, parent binding, attenuation, policy decisions/obligations, signed state checkpoints, receipt body/attestations, selective-disclosure proof, and transactional effect messages. Specify fail-closed behavior, validation order, stable message allocations, and required negative conformance cases.

- [ ] **Step 3: Define compatibility and extension boundaries**

The map must state:

```text
paper/1 record and message encoding -> DARI legacy compatibility profile
dari/1 kernel -> active protocol
dari.ai/1 -> provider-neutral inference
dari.tools/1 -> effects and tool bridges
dari.model-supply/1 -> model artifact and endpoint authorization
dari.web/1 -> schema only until runtime conformance exists
dari.federation/1 -> schema only until runtime conformance exists
```

The spec must prohibit claiming web or federation runtime conformance from schema publication alone.

- [ ] **Step 4: Verify every Phase 2 implementation object has a normative section**

```bash
rg -n 'Authorization Grant|attenuation|Authorization Decision|Signed State Checkpoint|Receipt Attestation|selective disclosure|Effect Prepare|dari\.ai/1|dari\.web/1|dari\.federation/1' docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md docs/plans/PAPER/DARI_COMPATIBILITY_AND_PROFILE_MAP.md
git diff --check -- docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md docs/plans/PAPER/DARI_COMPATIBILITY_AND_PROFILE_MAP.md
```

- [ ] **Step 5: Commit only the normative contract hunks**

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
- Produces byte-for-byte fixtures that Task 14 must continue to decode.

- [ ] **Step 1: Write a golden HELLO test**

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

- [ ] **Step 2: Capture current bytes and commit**

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

**Interfaces:**
- Produces `TrustBundle`, `NewPeerAuthenticator(TrustBundle)`, and `VerifyPeerProof(ctx context.Context, transcript []byte, proof *paper.AuthProofMessage) (*paper.PeerCredential, error)`.
- Test helper: `signedProofFixture(t *testing.T, transcript []byte) (*paper.AuthProofMessage, relay.TrustBundle)` creates an issuer, a signed credential, and a proof bound to the supplied transcript.

- [ ] **Step 1: Write valid, tampered, replayed, expired, and revoked proof tests**

```go
func TestPeerAuthenticatorRejectsAnotherTranscript(t *testing.T) {
    proof, trust := signedProofFixture(t, []byte("transcript-a"))
    verifier := relay.NewPeerAuthenticator(trust)
    if _, err := verifier.VerifyPeerProof(context.Background(), []byte("transcript-b"), proof); err == nil { t.Fatal("replayed proof accepted") }
}
```

- [ ] **Step 2: Implement the verifier**

Decode the COSE credential; verify issuer signature, validity, protocol version, and revocation epoch; verify proof of possession over transcript hash plus challenge ID. `PeerCredential.VerifySignature` must decode the COSE object and compare its payload byte-for-byte with `Credential.SigningBytes()` after signature verification; a valid signature over a different credential payload must fail. Remove `harnessID := string(proof.Credential)` and derive identity only from the verified credential.

- [ ] **Step 3: Terminate active sessions by revoked serial and verify**

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
- Produces `dari.ai/1`, `dari.tools/1`, `dari.model-supply/1`, `dari.web/1`, `dari.federation/1`, `NegotiateProfiles(local, remote []ProfileOffer) ([]ProfileResult, error)`, and `EXACT|DEGRADED|UNSUPPORTED`.

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

- [ ] **Step 3: Register bounded web and federation schemas**

Web declares origin binding, proof-of-possession, reconnect, and unchanged authorization/receipt semantics. Federation declares issuer, audience, trust domain, policy intersection, residency constraints, and receipt keys. Runtime support reports `UNSUPPORTED` until tested.

- [ ] **Step 4: Test and commit**

```bash
go test ./internal/paper ./adapters/... -run 'Profile|Capability|Adapter' -count=1
git add internal/paper/profiles.go internal/paper/profiles_test.go registry/extensions.csv registry/profiles.csv internal/paper/ai_v2.go internal/paper/models.go adapters/vllm/adapter.go adapters/sglang/adapter.go
git commit -m "feat: define vendor-neutral protocol profiles"
```

### Task 13: Replace proxy conformance tests with black-box invariants

**Files:**
- Modify: `conformance/conformance_test.go`
- Modify: `conformance/README.md`
- Create: `conformance/runner_test.go`
- Create: `conformance/manifest.json`
- Create: `conformance/testdata/dari1/`

**Interfaces:**
- Produces capability-scoped results and canonical/negative vectors.

- [ ] **Step 1: Make invariants 1–12 call real seams**

Remove comment-only tests and tests substituting credential time validity for authorization. Exercise verifier, Relay authorization, profile gate, effect lifecycle, and receipt verification.

- [ ] **Step 2: Add context-authorization and negative vectors**

Attempt excluded context and prove denial before the fake Inference Peer receives it. Add bad signature, broadened child, stale state, invalid critical field, modified receipt, and duplicate effect vectors.

- [ ] **Step 3: Publish exact support in `manifest.json`**

Each record names profile, test, normative requirement, and result. Do not report web/federation runtime support without runtime tests.

- [ ] **Step 4: Test and commit**

```bash
go test ./conformance -count=1
go test ./... -count=1
git add conformance
git commit -m "test: enforce protocol invariants end to end"
```

---

## Phase 3 — PAPER to DARI Rename

**Phase entry gate:** Before any `git mv`, every rename target must be free of uncommitted, non-task-owned changes in its owning repository. Compare each path against the `/tmp/dari-task-*-before.patch` snapshots. Commit or preserve task-owned work with patch staging; if unrelated user changes remain inseparable, stop the rename phase and report the exact paths instead of staging them through a move.

### Task 14: Rename the Go package with a bounded compatibility adapter

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

- [ ] **Step 1: Test ALPN ordering**

```go
func TestDARIListenerALPNs(t *testing.T) {
    if got := relay.SupportedALPNs(); !slices.Equal(got, []string{"dari/1", "paper/1"}) { t.Fatalf("unexpected ALPNs: %v", got) }
}
```

- [ ] **Step 2: Move with `git mv` and mechanically update package declarations, imports, selectors, logs, and exported comments**

Before moving, save the exact dependent-file inventory with `rg -l 'internal/paper|\bpaper\.' --glob '*.go' > /tmp/dari-root-dependent-go-files.txt`. Rename `PaperALPN`, `PaperListener`, and `NewPaperListener` to DARI equivalents. Do not alter message numbers, CBOR labels, persisted columns, or legacy fixture bytes.

- [ ] **Step 3: Isolate the legacy literal**

```go
package dari

const LegacyALPNProtocol = "paper/1"
```

- [ ] **Step 4: Test and commit**

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

### Task 15: Rename Patty Code's protocol client

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

New clients offer `dari/1` first. They accept `paper/1` only when compatibility is enabled and never drop authorization or receipt requirements.

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

### Task 16: Rename every active document, artifact, registry entry, comment, and UI label

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

### Task 17: Add open-spec governance and durable Patty attribution

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

### Task 18: Final release gate

**Files:**
- Modify only files proven defective by these checks.

**Interfaces:**
- Produces release-ready DARI source, PDFs, and bounded PAPER v1 compatibility.

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
go test ./internal/dari ./internal/relay ./internal/policy ./internal/provenance -run 'Auth|Grant|Delegation|Decision|Checkpoint|Receipt|Evidence|Effect|Compatibility' -count=1
```

- [ ] **Step 4: Rebuild and inspect papers**

```bash
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
git diff --check
git diff --stat
```

Confirm only the bounded compatibility/lineage locations from Task 16 match, every normative-document match is compatibility-only, no protected environment/secrets file changed, no unrelated user work is staged, and no LaTeX temporary file was added.

- [ ] **Step 6: Commit verification fixes only when checks required source changes**

```bash
git commit -m "chore: verify DARI migration and compatibility"
```

Skip the commit if verification produced no source change.

---

## Release Gates

### Gate A — Paper

- English and Korean PDFs build without undefined references.
- The claim matrix distinguishes implemented, implementation-required, and extension-profile claims.
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

### Gate C — Rename

- Active packages, symbols, comments, docs, UI, registries, SDK examples, LaTeX, PDFs, and archives use DARI.
- `paper/1` is accepted only by the compatibility adapter.
- Numeric message IDs and golden legacy vectors remain stable.
- Root and nested-repository tests pass.
- Environment and secrets files remain untouched.
