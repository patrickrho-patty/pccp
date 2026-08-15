# DARI — Governance, Origin, and Compatibility

**License:** Apache License 2.0 (`LICENSE`). Applies to the DARI
protocol specification (`docs/plans/DARI/DARI_Protocol_Specification_v1.0.md`
and its companion documents) and this reference implementation.

**Decision:** Apache-2.0 selected by the steward (2026-08-15) for the
patent grant, attribution, and permissive-implementation profile
standard for open protocols. Recorded as the Task 23 Step-1 decision.

## Origin and attribution (factual)

DARI — **D**elegated **A**uthorization and **R**eceipts for **I**nference —
originated at **Patty Co., Ltd.** It evolved from the company's
earlier internal protocol implementation (known internally as "PAPER");
the compatibility profile preserving that deployed record is frozen in
`internal/dari/legacy_paper1.go` and documented in
`docs/plans/DARI/PAPER_TO_DARI_MIGRATION.md`.

**Citation norm:** implementations deriving from this work should
cite:

> DARI: Delegated Authorization and Receipts for Inference.
> Patty Co., Ltd., 2026. Specification 1.0 (Apache-2.0).

The English and Korean research manuscripts
(`docs/plans/DARI/arxiv/DARI_arXiv_Paper_v1.0.md`, `..._KO.md`) are
the narrative references; Appendix F of the specification is the
normative contract.

## Contribution mechanics

- Contributions are accepted under the Apache-2.0 terms (Section 5):
  submitting a Contribution constitutes acceptance of these terms.
- Normative changes (Appendix F, the Compatibility and Profile Map,
  message/object registries) additionally require a conformance-vector
  update: the `conformance/runner_test.go` negative case that pins the
  changed behavior, and `conformance/manifest.json` updated to cite it.
  Registry renumbering of deployed values is prohibited
  (compatibility map §6).
- The connector mirror (`patty-code-pccccp`'s `internal/dariproto`)
  must stay byte-compatible: kernel changes land in both repositories
  in one logical change, pinned by the cross-repo conformance suites
  (`internal/lease_conformance`, `internal/provenance_conformance`).

## Fair compatibility language

Independent implementations of `dari/1` are welcome. To claim
conformance:

1. Pass the F.14 negative cases (the black-box runner in
   `conformance/runner_test.go` is the reference battery).
2. Preserve the frozen `paper/1` compatibility bytes exactly, or
   reject `paper/1` explicitly — never blend the kernels.
3. Report capability support honestly per the profile registry
   (EXACT / DEGRADED / UNSUPPORTED); `UNSUPPORTED` is a valid,
   respectable answer and must be recorded with its reason.

The "DARI" name and Patty Co., Ltd. marks may be used to describe
provenance and compatibility, not to imply endorsement (License
Section 6).
