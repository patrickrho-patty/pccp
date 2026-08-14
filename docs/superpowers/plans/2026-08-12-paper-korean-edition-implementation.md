# PAPER Korean Edition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete Korean scholarly edition of the DARI manuscript as `DARI_arXiv_KO.pdf`.

**Architecture:** Keep the English manuscript immutable and create a parallel XeLaTeX entry point. Reuse empirical data, bibliography, equations, and conceptual PNGs; localize editable diagram/chart sources and all manuscript prose.

**Tech Stack:** XeLaTeX, kotex, fontspec, NanumMyeongjo, Noto Sans CJK KR, BibTeX, PGFPlots, TikZ, Poppler.

## Global Constraints

- Translate the complete manuscript, including appendix and limitations.
- Preserve claim strength, measurements, equations, citations, and identifiers.
- Use native Korean academic prose rather than word-for-word translation.
- Do not modify `main.tex` or `DARI_arXiv.pdf`.
- Do not touch unrelated concurrent application changes.

---

### Task 1: Create Korean typesetting and localized editable figures

**Files:**
- Create: `docs/plans/PAPER/arxiv/main_ko.tex`
- Create: `docs/plans/PAPER/arxiv/figures/model-identity-chain-ko.tex`
- Create: `docs/plans/PAPER/arxiv/figures/primitive-latency-plot-ko.tex`

- [x] Configure XeLaTeX, kotex, NanumMyeongjo, and Noto Sans CJK KR.
- [x] Localize editable figure and chart labels without changing data or topology.
- [x] Preserve the existing conceptual PNGs and translate their captions.

### Task 2: Adapt the complete manuscript into Korean academic prose

**Files:**
- Complete: `docs/plans/PAPER/arxiv/main_ko.tex`

- [x] Translate title, abstract, body, tables, captions, ethics, reproducibility, conclusion, and appendix.
- [x] Preserve all equations, labels, citations, measurements, caveats, and evidence boundaries.
- [x] Run terminology and coverage checks against `main.tex`.

### Task 3: Build and inspect the Korean PDF

**Files:**
- Generate: `docs/plans/PAPER/arxiv/DARI_arXiv_KO.pdf`

- [ ] Build with XeLaTeX and BibTeX until references stabilize.
- [ ] Check embedded fonts, Hangul extraction, references, citations, and layout diagnostics.
- [ ] Compare structural counts with the English paper and visually inspect every page.
- [ ] Conduct a final Korean scholarly-language review.
