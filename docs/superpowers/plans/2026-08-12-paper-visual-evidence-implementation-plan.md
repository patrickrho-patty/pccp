# PAPER Visual Evidence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the original PAPER arXiv publication scope with four evidence-disciplined figures, missing related-work support, reproducibility metadata, and a verified portable PDF/source package.

**Architecture:** Keep the Qwen PNG as the sole generated conceptual artwork. Add two focused TikZ source files for protocol-design diagrams and one PGFPlots source file that reads the checked-in TSV directly; include all three from `main.tex` so the arXiv build remains offline and vector-native. Reconcile the publication plan only after the PDF and archive prove each item complete.

**Tech Stack:** pdfLaTeX, TikZ, PGFPlots/PGFPlotstable, BibTeX, latexmk, standard TeX Live packages, TSV benchmark data, Poppler QA tools.

## Global Constraints

- Work directly on the current branch; do not create a branch or worktree.
- Do not modify environment or secret files.
- Preserve the PAPER method and all current evidence boundaries.
- Use the Qwen Image 2 PNG as the lead conceptual figure.
- Generate empirical plots only from `benchmark-data/core-primitives.tsv`.
- Do not claim B0--B4, TTFT, scale, adversarial, or provenance-fidelity results.
- Use no proprietary fonts, network access during build, or shell escape.
- Do not modify unrelated workspace files.

---

### Task 1: Vector Protocol Diagrams

**Files:**
- Create: `docs/plans/PAPER/arxiv/figures/governed-exchange-lifecycle.tex`
- Create: `docs/plans/PAPER/arxiv/figures/model-identity-chain.tex`
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: lifecycle and model-resolution semantics already stated in `main.tex`.
- Produces: `fig:lifecycle` and `fig:model-identity`, rendered by `\input{...}` with shared TikZ libraries and no external conversion step.

- [ ] Add `tikz`, the `arrows.meta`, `positioning`, `fit`, and `calc` libraries, and shared blue-gray/amber colors to the LaTeX preamble.
- [ ] Draw the six required lifecycle states in order, annotate the authority gate beneath each state, and mark tool/approval as optional without making authorization optional.
- [ ] Draw the five-node model chain and label Catalog Model, PMP, and Endpoint Lease as product, artifact, and deployment identity respectively.
- [ ] Insert each diagram next to its defining prose with a caption that labels it a protocol-design figure rather than an implementation result.
- [ ] Run `make clean && make && make check`; require zero undefined references and zero overfull boxes.

### Task 2: Data-Derived Benchmark Plot

**Files:**
- Create: `docs/plans/PAPER/arxiv/figures/primitive-latency-plot.tex`
- Modify: `docs/plans/PAPER/arxiv/main.tex`
- Modify: `docs/plans/PAPER/arxiv/Makefile`

**Interfaces:**
- Consumes: columns `benchmark` and `median_ns_per_op` from `benchmark-data/core-primitives.tsv`.
- Produces: `fig:primitive-latency`, a log-scale bar plot whose y-values are read at LaTeX build time from the TSV.

- [ ] Add `pgfplots` and `pgfplotstable` with `compat=1.18` to the preamble.
- [ ] Build a seven-bar log-scale plot in ns/op with readable abbreviated operation labels and a caption that excludes network, policy, storage, model, GPU, and token-generation costs.
- [ ] Reference the plot in the results prose while retaining Table `tab:microbench` as the exact-value record.
- [ ] Add the three `.tex` figure sources and TSV as explicit PDF dependencies in the Makefile.
- [ ] Compare the seven plotted coordinates in the LaTeX source/data path against the seven TSV medians and require exact correspondence.

### Task 3: Related Work and Reproducibility Corrections

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`
- Modify: `docs/plans/PAPER/arxiv/references.bib`
- Modify: `docs/plans/PAPER/arxiv/README.md`

**Interfaces:**
- Consumes: RFC 9943, evaluated commit `0234034b51fb6f1a5127280ce1e9c209b8198004`, and existing benchmark metadata.
- Produces: resolved `rfc9943` citation and self-contained artifact instructions that disclose the missing raw per-run capture.

- [ ] Add the official RFC 9943 BibTeX record and position SCITT as complementary transparency for signed artifact statements, not live exchange authority.
- [ ] Update the figure disclosure: Qwen is generated conceptual artwork; TikZ figures are protocol-design diagrams; PGFPlots is data-derived empirical visualization.
- [ ] Document the evaluated commit, Apple M4 Pro/14 logical CPUs/48 GiB/macOS 27.0/arm64/Go 1.26.5 environment, exact commands, median-only dataset, and absent raw five-run output in README.
- [ ] Run BibTeX through `make`; require the SCITT citation to resolve and all reproduction claims to match the TSV/manuscript.

### Task 4: Publication-Plan Reconciliation

**Files:**
- Modify: `docs/superpowers/plans/2026-08-12-paper-arxiv-publication-plan.md`

**Interfaces:**
- Consumes: verified artifacts from Tasks 1--3.
- Produces: an accurate completed checklist with scientific limitations kept separate from publication deliverables.

- [ ] Replace the stale two-column requirement with the approved readable single-column, one-inch-margin, embedded Times-compatible typography.
- [ ] Change the visual checklist to name the conceptual overview, lifecycle, model identity, benchmark plot, and evidence tables explicitly.
- [ ] Mark only verified publication tasks complete.
- [ ] Add a concise remaining-scientific-work note for B0--B4, production authentication, adversarial enforcement, scale/TTFT, interoperability, and provenance fidelity.

### Task 5: PDF and Archive Verification

**Files:**
- Produce: `docs/plans/PAPER/arxiv/PAPER_arXiv.pdf`
- Produce: `docs/plans/PAPER/arxiv/PAPER-arXiv-source.tar.gz`

**Interfaces:**
- Consumes: all publication sources and data from Tasks 1--4.
- Produces: visually reviewed PDF and independently rebuildable source archive.

- [ ] Run `make clean && make && make check && make archive` from the publication directory.
- [ ] Confirm with `pdfinfo` that the PDF is US Letter and non-empty; confirm with `pdffonts` that every font is embedded.
- [ ] Render every page with `pdftoppm`, create a contact sheet, and inspect every page plus each new figure at original resolution for clipping, illegible labels, table overflow, orphan headings, and blank regions.
- [ ] Extract PDF text and confirm all four figure captions, SCITT, benchmark limitations, and appendix headings are present.
- [ ] Unpack `PAPER-arXiv-source.tar.gz` into a fresh temporary directory and run `make clean all check` there with exit code 0.
- [ ] Run `git diff --check` on authored sources and report unrelated workspace changes without modifying them.
