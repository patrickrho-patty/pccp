# PAPER arXiv Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a self-contained, evidence-qualified arXiv LaTeX paper and a visually reviewed PDF from the current PAPER manuscript, PCCP v2 plan, and local implementation evidence.

**Architecture:** Keep publication sources under `docs/plans/PAPER/arxiv/`. Use one principal `main.tex`, one BibTeX database, checked-in figures/data, and a Makefile that compiles without network access or shell escape. Report implemented, partial, and design-only claims separately.

**Tech Stack:** pdfLaTeX, BibTeX, latexmk, standard TeX Live packages, PNG/PDF figures, Go benchmark output.

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

- [ ] Create the directory structure and copy the selected Qwen figure.
- [ ] Add a neutral two-column `article` preamble using standard TeX Live packages.
- [ ] Add deterministic `make`, `make clean`, and `make check` commands.
- [ ] Run `make` and require a zero exit status.

### Task 2: Evidence and Claim Matrix

**Files:**
- Create: `docs/plans/PAPER/arxiv/benchmark-data/core-primitives.tsv`
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: five-run Go benchmark medians, test results, and reviewed implementation limitations.
- Produces: implementation-status, claim-to-evidence, and preliminary-results tables.

- [ ] Record hardware, Go version, commit, commands, medians, allocations, and scope limitations.
- [ ] Add a claim-to-evidence table separating implemented, partial, and design-only claims.
- [ ] Add an invariant-coverage table that identifies missing or placeholder coverage.
- [ ] Verify every numerical value against captured command output.

### Task 3: Manuscript Revision

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: PAPER v1 manuscript, PAPER PRD, PCCP v2 protocol-relevant changes, and Task 2 evidence.
- Produces: a coherent prototype-and-preliminary-evaluation paper.

- [ ] Write the abstract and introduction around governance-native exchanges and measured prototype foundations.
- [ ] Add related-work boundaries for model APIs, MCP, A2A, policy systems, and supply-chain provenance.
- [ ] Present threat model, architecture, Capability Lease, Policy Epoch, Relay Verdict, and Provenance Spine.
- [ ] Add Catalog Model, Model Descriptor, Catalog Epoch, PMP, and PIA endpoint resolution.
- [ ] Describe the actual Go prototype and explicitly mark incomplete authentication and permissive pipeline stages.
- [ ] Add methodology, preliminary results, discussion, ethics, limitations, reproducibility, and conclusion.

### Task 4: Figures and Tables

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`
- Create: `docs/plans/PAPER/arxiv/figures/qwen-governed-exchange.png`

**Interfaces:**
- Consumes: selected Qwen image and evidence tables.
- Produces: readable figures/tables with captions and labels.

- [ ] Place the Qwen figure at full text width with an accurate caption that identifies it as a conceptual overview.
- [ ] Add architecture, model-identity, comparison, benchmark, and limitations tables using LaTeX-native structures.
- [ ] Ensure every figure/table is referenced from the prose.
- [ ] Compile and inspect for overflow and illegible text.

### Task 5: Bibliography and Claim Support

**Files:**
- Modify: `docs/plans/PAPER/arxiv/references.bib`
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: official specifications and primary research sources already identified by the manuscript.
- Produces: resolved citations with no undefined-reference warnings.

- [ ] Add BibTeX entries for QUIC, TLS 1.3, CBOR, COSE, HPKE, MCP, A2A, SPIFFE, in-toto, SLSA, SCITT, and relevant governance/provenance research.
- [ ] Replace numeric Markdown references with semantic citation keys.
- [ ] Run BibTeX through latexmk and require zero undefined citation warnings.

### Task 6: PDF Build and Visual QA

**Files:**
- Produce: `docs/plans/PAPER/arxiv/PAPER_arXiv.pdf`

**Interfaces:**
- Consumes: final LaTeX, bibliography, and figures.
- Produces: the requested polished PDF.

- [ ] Run `make clean && make`.
- [ ] Run `pdfinfo PAPER_arXiv.pdf` and record page count and page size.
- [ ] Render every PDF page to PNG with `pdftoppm`.
- [ ] Inspect every page for clipping, blank regions, table overflow, orphan headings, and unreadable figures.
- [ ] Correct all visual defects and rebuild.

### Task 7: arXiv Portability and Final Verification

**Files:**
- Modify: `docs/plans/PAPER/arxiv/README.md`
- Produce: `docs/plans/PAPER/arxiv/PAPER-arXiv-source.tar.gz`

**Interfaces:**
- Consumes: verified publication directory.
- Produces: reproducible source archive and build instructions.

- [ ] Document exact build, benchmark, hardware, and source-archive commands.
- [ ] Check for absolute paths, missing assets, shell escape, and nonstandard fonts.
- [ ] Build from a temporary unpacked source archive.
- [ ] Run `make check` and require no undefined citations, missing references, or overfull boxes.
- [ ] Report remaining scientific limitations separately from formatting/build status.
