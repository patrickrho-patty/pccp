# PAPER arXiv Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a self-contained, evidence-qualified arXiv LaTeX paper and a visually reviewed PDF from the current PAPER manuscript, PCCP v2 plan, and local implementation evidence.

**Architecture:** Keep publication sources under `docs/plans/PAPER/arxiv/`. Use one principal `main.tex`, one BibTeX database, checked-in figures/data, and a Makefile that compiles without network access or shell escape. Report implemented, partial, and design-only claims separately.

**Tech Stack:** pdfLaTeX, BibTeX, latexmk, TikZ/PGFPlots, standard TeX Live packages, PNG/vector-native figures, Go benchmark data.

## Global Constraints

- Work directly on the current branch; do not create a branch or worktree.
- Do not modify environment or secret files.
- Preserve the PAPER method; improve presentation and evidence discipline.
- Use the Qwen Image 2 PNG as the lead conceptual figure.
- Generate empirical plots only from measured data.
- Do not describe PRD requirements or scaffolding as implemented results.
- The LaTeX build must use no proprietary fonts, network downloads, or shell escape.

---

### Task 1: Publication Source Skeleton

**Files:**
- Create: `docs/plans/PAPER/arxiv/main.tex`
- Create: `docs/plans/PAPER/arxiv/references.bib`
- Create: `docs/plans/PAPER/arxiv/Makefile`
- Create: `docs/plans/PAPER/arxiv/README.md`
- Create: `docs/plans/PAPER/arxiv/figures/`
- Create: `docs/plans/PAPER/arxiv/benchmark-data/`

**Interfaces:**
- Consumes: current Markdown manuscript and Qwen figure.
- Produces: an offline-compilable publication directory used by every later task.

- [x] Create the directory structure and copy the selected Qwen figure.
- [x] Add a readable single-column `article` preamble with one-inch margins, embedded Times-compatible text/math fonts, and standard TeX Live packages.
- [x] Add deterministic `make`, `make clean`, and `make check` commands.
- [x] Run `make` and require a zero exit status.

### Task 2: Evidence and Claim Matrix

**Files:**
- Create: `docs/plans/PAPER/arxiv/benchmark-data/core-primitives.tsv`
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: five-run Go benchmark medians, test results, and reviewed implementation limitations.
- Produces: implementation-status, claim-to-evidence, and preliminary-results tables.

- [x] Record hardware, Go version, commit, commands, medians, allocations, and scope limitations.
- [x] Add a claim-to-evidence table separating implemented, partial, and design-only claims.
- [x] Add an invariant-coverage table that identifies missing or placeholder coverage.
- [x] Verify every numerical value against captured command output.

### Task 3: Manuscript Revision

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: PAPER v1 manuscript, PAPER PRD, PCCP v2 protocol-relevant changes, and Task 2 evidence.
- Produces: a coherent prototype-and-preliminary-evaluation paper.

- [x] Write the abstract and introduction around governance-native exchanges and measured prototype foundations.
- [x] Add related-work boundaries for model APIs, MCP, A2A, policy systems, and supply-chain provenance/transparency.
- [x] Present threat model, architecture, Capability Lease, Policy Epoch, Relay Verdict, and Provenance Spine.
- [x] Add Catalog Model, Model Descriptor, Catalog Epoch, PMP, and PIA endpoint resolution.
- [x] Describe the actual Go prototype and explicitly mark incomplete authentication and permissive pipeline stages.
- [x] Add methodology, preliminary results, discussion, ethics, limitations, reproducibility, and conclusion.

### Task 4: Figures and Tables

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`
- Create: `docs/plans/PAPER/arxiv/figures/qwen-governed-exchange.png`

**Interfaces:**
- Consumes: selected Qwen image and evidence tables.
- Produces: readable figures/tables with captions and labels.

- [x] Place the Qwen figure at full text width with an accurate caption that identifies it as a conceptual overview.
- [x] Add LaTeX-native Governed Exchange lifecycle and model-identity-chain diagrams.
- [x] Add a PGFPlots benchmark chart read directly from the checked-in TSV.
- [x] Add authority-object, comparison, benchmark-detail, claim-boundary, and invariant-coverage tables.
- [x] Ensure every figure/table is referenced from the prose.
- [x] Compile and inspect for overflow and illegible text.

### Task 5: Bibliography and Claim Support

**Files:**
- Modify: `docs/plans/PAPER/arxiv/references.bib`
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: official specifications and primary research sources already identified by the manuscript.
- Produces: resolved citations with no undefined-reference warnings.

- [x] Add BibTeX entries for QUIC, TLS 1.3, CBOR, COSE, HPKE, MCP, A2A, SPIFFE, in-toto, SLSA, SCITT, and relevant governance/provenance research.
- [x] Replace numeric Markdown references with semantic citation keys.
- [x] Run BibTeX through latexmk and require zero undefined citation warnings.

### Task 6: PDF Build and Visual QA

**Files:**
- Produce: `docs/plans/PAPER/arxiv/PAPER_arXiv.pdf`

**Interfaces:**
- Consumes: final LaTeX, bibliography, and figures.
- Produces: the requested polished PDF.

- [x] Run `make clean && make`.
- [x] Run `pdfinfo PAPER_arXiv.pdf` and record page count and page size.
- [x] Render every PDF page to PNG with `pdftoppm`.
- [x] Inspect every page for clipping, blank regions, table overflow, orphan headings, and unreadable figures.
- [x] Correct all visual defects and rebuild.

### Task 7: arXiv Portability and Final Verification

**Files:**
- Modify: `docs/plans/PAPER/arxiv/README.md`
- Produce: `docs/plans/PAPER/arxiv/PAPER-arXiv-source.tar.gz`

**Interfaces:**
- Consumes: verified publication directory.
- Produces: reproducible source archive and build instructions.

- [x] Document exact build, benchmark, hardware, and source-archive commands.
- [x] Check for absolute paths, missing assets, shell escape, and nonstandard fonts.
- [x] Build from a temporary unpacked source archive.
- [x] Run `make check` and require no undefined citations, missing references, or overfull boxes.
- [x] Report remaining scientific limitations separately from formatting/build status.

## Remaining Scientific Work

The publication package is complete, but the paper remains scientifically preliminary until it reports B0--B4 end-to-end comparisons, production authentication and negative enforcement tests, Relay/TTFT/scale results, independent interoperability, and provenance-fidelity measurements. Raw per-run output must also accompany the next empirical release; the present artifact retains verified medians and metadata only.
