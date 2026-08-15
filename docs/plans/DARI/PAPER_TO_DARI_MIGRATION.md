# PAPER → DARI Migration Record

**Date:** 2026-08-14 · **Master plan:** Task 21 (connector client) + Task 22 (repository-wide naming)

The protocol was renamed from PAPER to DARI. This file is the historical narrative and the only doc (besides the bounded compatibility sections) that keeps the legacy name in prose.

## What changed

- **Packages:** `internal/paper` → `internal/dari` (relay); `internal/paperproto` → `internal/dariproto`, `internal/provider/paper` → `internal/provider/dari` (connector).
- **ALPN:** canonical `dari/1`, negotiated first. Legacy `paper/1` remains accepted (frozen in `legacy_paper1.go`, pinned by `compatibility_test.go` in both repos).
- **Wire preface:** the 8-byte preface still spells the historical name; the bytes are a frozen compatibility artifact.
- **Hash/signing domains:** `DARI-AUTH-v1`, `DARI-OBJ-v1` (changed in lockstep in both repos; previously issued in-memory credentials/leases re-issue fresh on connect).
- **Crypto profile string:** `DARI-BASE-1`.
- **Extensions:** `dari.ai/1`, `dari.models/1`.
- **Env vars:** `DARI_HARNESS_{CREDENTIAL_FILE,KEY,ID}`, `PCCP_RELAY_DARI_ADDR`, `PCCP_PIA_DARI_ADDR` — with `PAPER_*` legacy fallbacks during the migration window.
- **Provider kind:** `dari` canonical; `paper` resolves to it (legacy configs keep working).
- **Docs/artifacts:** `docs/plans/PAPER/` → `docs/plans/DARI/`, `PAPER.md` → `DARI.md`, the three `PAPER_*v1.0.md` → `DARI_*v1.0.md`, arXiv targets `DARI_arXiv{,_KO}.pdf` + `DARI-arXiv-source.tar.gz`.

## What did NOT change (compatibility invariants)

- Message-type numbering, framing layout, CBOR schemas, COSE-Sign1 envelope, lease/epoch/catalog/receipt bodies — byte-identical.
- The 8-byte connection preface.
- The frozen `paper/1` cryptographic domain strings (`PAPER-AUTH-v1`, `PAPER-OBJ-v1\0`, `PAPER-CHUNK-v1\0`, `PAPER-EVIDENCE-*-v1`) — recorded as named constants in `legacy_paper1.go`.

## Domain-string semantics (read this before interoperating cross-version)

The legacy spec body computes its transcript/object/chunk/evidence domains with `PAPER-*` strings; the normative Appendix F kernel (`dari/1`) defines `DARI-*` domains. Current builds compute the **DARI-\* domains** (both endpoints deploy together; credentials and leases are re-issued on connect, so the live e2e suites verify the pair). A **pre-rename** peer must be reached on the legacy profile with the frozen `PAPER-*` bytes; selecting the domain set by negotiated ALPN arrives with profile negotiation (master-plan Tasks 7+).

## Connector registry reconciliation (compat map §12)

The duplicated client package's known drift was reconciled to the deployed root registry: `CLOSE` moved to `0x0006` (with `DRAIN` at `0x0005`), the model-catalog block moved to `0x0D00–0x0D09`, admin/broadcast to `0x0B00–0x0B02`, provenance to `0x0700–0x0703`, receipts to `0x0307/0x0308`, leases to `0x0210–0x0212`. `registry/messages.csv` was updated to corroborate (including the normative `0x0610–0x0614` DARI effect allocations). Extension identifiers now follow the profile registry (`dari.ai/1`, `dari.model-supply/1`).

## Validation

- Both repos' full test suites green.
- `DARI_LIVE_E2E=1` (mock PIA) and `DARI_LIVE_E2E_LIVE=1` (real yolo-auto / qwen3.6-35b-a3b) end-to-end loops pass against a freshly built relay binary: enroll → auth → epoch/catalog/lease → governed streaming inference → evidence receipt + ack → provenance ingestion.

## Declared historical archives (legacy name retained by design)

- `docs/superpowers/plans/2026-08-12-*.md`, `2026-08-13-paper-*.md`, `docs/superpowers/specs/2026-08-13-paper-*.md` — point-in-time planning artifacts written before the rename; rewriting them would falsify history.
- `docs/plans/DARI/PAPER_TO_DARI_MIGRATION.md` (this file).
- `docs/plans/DARI/DARI_COMPATIBILITY_AND_PROFILE_MAP.md` — compatibility mapping only.
- Compatibility sections of `DARI_Protocol_Specification_v1.0.md`.
- `internal/dari/legacy_paper1.go`, `internal/dari/compatibility_test.go` (and the connector's `dariproto` equivalents) — the frozen `paper/1` surface.
