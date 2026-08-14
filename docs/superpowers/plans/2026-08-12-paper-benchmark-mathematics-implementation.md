# PAPER Benchmark Mathematics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the clipped benchmark label and strengthen the paper with a restrained set of evidence-bearing equations supported by the existing artifact.

**Architecture:** Keep PGFPlots bound directly to the checked-in TSV and add axis headroom rather than rasterizing the empirical figure. Add formal definitions beside the protocol and benchmark prose they clarify, without introducing new measurements or inferential statistics.

**Tech Stack:** LaTeX, AMSMath, PGFPlots/PGFPlotstable, latexmk, BibTeX, Poppler.

## Global Constraints

- Work directly on the current branch; do not create a worktree or branch.
- Do not change benchmark observations in `benchmark-data/core-primitives.tsv`.
- Do not infer variance, confidence intervals, or significance from retained medians.
- Keep changes confined to the DARI arXiv package and this plan.

---

### Task 1: Repair the benchmark plot

**Files:**
- Modify: `docs/plans/PAPER/arxiv/figures/primitive-latency-plot.tex`

**Interfaces:**
- Consumes: `median_ns_per_op` from `benchmark-data/core-primitives.tsv`
- Produces: Figure 4 with all seven exact values and unclipped labels

- [x] Increase the logarithmic y-axis ceiling above the largest measured value.
- [x] Add a uniform label offset that keeps values clear of bars and the frame.
- [x] Build and render the benchmark page to verify the 97,016 label is inside the border.

### Task 2: Formalize protocol and benchmark calculations

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: the existing state-machine semantics and retained five-run benchmark medians
- Produces: defined authorization, median, normalized-cost, throughput, and allocation equations

- [x] Add the governed-transition predicate and define every term in adjacent prose.
- [x] Define the five-run median without claiming unavailable dispersion.
- [x] Define size-normalized cost, effective in-memory throughput, and allocation amplification.
- [x] Replace the inaccurate approximately-linear wording with a claim supported by the three retained points.
- [x] Remove the duplicated migration paragraph (already absent in the live manuscript at implementation time).

### Task 3: Rebuild and validate publication artifacts

**Files:**
- Regenerate: `docs/plans/PAPER/arxiv/DARI_arXiv.pdf`
- Regenerate: `docs/plans/PAPER/arxiv/DARI-arXiv-source.tar.gz`

**Interfaces:**
- Consumes: the revised LaTeX source, plot, figures, bibliography, and dataset
- Produces: an offline-buildable arXiv package and publication PDF

- [x] Run `make clean && make && make check && make archive`.
- [x] Compile the archive in a fresh directory.
- [x] Confirm all plotted numbers match the TSV and no undefined references, citations, or overfull boxes exist.
- [x] Render and inspect all pages, with full-resolution inspection of the benchmark page.
