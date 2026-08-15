# PAPER Professional Rhetoric Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Revise the complete English and Korean PAPER manuscripts so that product significance is communicated through evidence-led academic prose rather than defensive, slogan-like, or marketing language.

**Architecture:** Treat `main.tex` as the canonical claim structure and audit it section by section. Adapt every changed claim into native Korean scholarly prose in `main_ko.tex`, preserving measurements, citations, labels, equations, tables, and claim strength.

**Tech Stack:** LaTeX, XeLaTeX, BibTeX, `latexmk`, Poppler PDF inspection tools.

## Global Constraints

- Preserve all technical claims, measurements, equations, citations, labels, figures, and tables unless wording alone is inaccurate.
- Promote PAPER through concrete implementation and benchmark evidence, not self-congratulatory language.
- Retain explicit scientific scope where required, but express it affirmatively as an evidence or assurance profile.
- Keep English and Korean editions structurally and semantically synchronized.
- Use native Korean academic syntax rather than translated English rhetoric.
- Do not touch unrelated workspace changes.

---

### Task 1: Audit the English manuscript

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: approved product-positioning design and current empirical claims
- Produces: canonical evidence-led English manuscript

- [x] Read the manuscript end to end and classify defensive negation, slogans, hype, reader-directed persuasion, and unnecessary meta-commentary.
- [x] Rewrite each finding as a direct research claim supported by implementation, protocol design, comparison, or measured evidence.
- [x] Confirm that measurements, citations, equations, labels, figures, tables, and assurance boundaries retain their meaning.

### Task 2: Audit and adapt the Korean manuscript

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main_ko.tex`

**Interfaces:**
- Consumes: revised English claim structure
- Produces: native Korean scholarly manuscript with equivalent evidentiary force

- [x] Read the Korean manuscript end to end for promotional, defensive, translated, or conversational constructions.
- [x] Reconstruct affected paragraphs in concise Korean research prose rather than translating English syntax line by line.
- [x] Confirm one-to-one parity of claims, measurements, citations, labels, equations, figures, and tables.

### Task 3: Regenerate and validate both publication artifacts

**Files:**
- Rebuild: `docs/plans/PAPER/arxiv/DARI_arXiv.pdf`
- Rebuild: `docs/plans/PAPER/arxiv/DARI_arXiv_KO.pdf`

**Interfaces:**
- Consumes: revised LaTeX manuscripts
- Produces: publication-ready English and Korean PDFs

- [x] Build both editions with `latexmk -xelatex` and require successful exit status.
- [x] Require zero undefined citations/references, overfull boxes, missing glyphs, and LaTeX errors.
- [x] Compare structural counts and label/citation sets across both editions.
- [x] Render and visually inspect the abstract, evaluation, discussion, conclusion, and appendix pages.
- [x] Extract PDF text and verify that flagged marketing constructions no longer appear.
