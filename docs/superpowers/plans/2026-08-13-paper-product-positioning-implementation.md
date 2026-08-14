# PAPER Product-Positioning Revision Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Historical notice:** This positioning plan is superseded by the DARI full-enterprise execution plan at `docs/superpowers/plans/2026-08-14-dari-protocol-evolution-implementation.md`. It remains a record of the earlier paper revision and does not authorize a prototype or schema-only release.

**Goal:** Publish synchronized English and Korean arXiv manuscripts that present PAPER as an open, product-integrated AI communication system with reusable SDK and serving-engine support.

**Architecture:** Treat `main.tex` as the canonical claim structure and adapt `main_ko.tex` section-for-section in native Korean academic prose. Preserve measured data and scientific scope while replacing stale prototype framing with repository-backed product integration evidence.

**Tech Stack:** LaTeX, XeLaTeX, BibTeX, TikZ/PGFPlots, Go repository evidence, Poppler visual inspection tools.

## Global Constraints

- Work directly in the current branch; do not create a worktree or branch.
- Do not modify environment or secrets files.
- Preserve unrelated working-tree changes.
- Do not claim external certification, independent security review, large-scale customer adoption, or unmeasured end-to-end performance.
- State that PAPER is open, Patty Code is the flagship full integration, and vLLM/SGLang plus the PIA SDK enable general AI applications including chat.
- Keep English and Korean claims, measurements, tables, citations, labels, and structure aligned.

---

### Task 1: Rewrite the English product narrative

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main.tex`

**Interfaces:**
- Consumes: approved design in `docs/superpowers/specs/2026-08-13-paper-product-positioning-design.md`
- Produces: canonical English claims and structure for the Korean adaptation

- [ ] **Step 1: Replace stale positioning globally**

Rewrite the PDF metadata, abstract, contributions, threat-model scope, implementation framing, evaluation headings/captions, limitations, reproducibility, conclusion, and appendix so “prototype,” “preliminary artifact,” and “development vertical slice” no longer define the system.

- [ ] **Step 2: Add product and open-adoption evidence**

Add explicit text covering the official Patty Code Harness integration, DARI-only service inference, open specification, Go protocol implementation, PIA SDK, vLLM/SGLang adapters, custom engines, and coding/chat/automation application classes.

- [ ] **Step 3: Replace the evidence-boundary table**

Create an implemented product-surface table that distinguishes code/product availability from the narrower scope of empirical measurements and deployment-profile assurance.

- [ ] **Step 4: Preserve scientific boundaries**

Keep primitive benchmark values unchanged. Recast authentication defaults, scale/TTFT, independent interoperability, and provenance-fidelity measurements as explicit assurance or empirical scope boundaries rather than unfinished-product claims.

- [ ] **Step 5: Run source checks**

Run literal scans for stale positioning, required product terms, duplicate labels, and LaTeX whitespace errors.

### Task 2: Produce the synchronized Korean manuscript

**Files:**
- Modify: `docs/plans/PAPER/arxiv/main_ko.tex`

**Interfaces:**
- Consumes: completed English claim structure from Task 1
- Produces: publication-grade Korean manuscript with equivalent meaning

- [ ] **Step 1: Adapt every revised section**

Render the English product position in native Korean academic language. Preserve technical identifiers where standard and avoid literal translated-English syntax.

- [ ] **Step 2: Mirror product/adoption evidence**

Include the open specification, official Patty Code integration, DARI-only path, PIA SDK, vLLM/SGLang adapters, custom engines, and chat/general-AI application classes with the same evidentiary strength as English.

- [ ] **Step 3: Mirror the product-surface and assurance tables**

Keep row coverage, status meanings, measurements, citations, labels, and section hierarchy aligned with English.

- [ ] **Step 4: Run parity checks**

Compare section/subsection, figure/table, equation, citation, and label counts and scan Korean source for stale prototype framing.

### Task 3: Build and review both publication PDFs

**Files:**
- Generate: `docs/plans/PAPER/arxiv/DARI_arXiv.pdf`
- Generate: `docs/plans/PAPER/arxiv/DARI_arXiv_KO.pdf`

**Interfaces:**
- Consumes: revised English and Korean LaTeX manuscripts
- Produces: final downloadable publication PDFs

- [ ] **Step 1: Build English from a clean LaTeX pass sequence**

Run XeLaTeX, BibTeX, and two final XeLaTeX passes with halt-on-error.

- [ ] **Step 2: Build Korean from a clean LaTeX pass sequence**

Run XeLaTeX, BibTeX, and two final XeLaTeX passes with halt-on-error.

- [ ] **Step 3: Validate PDF internals**

Check metadata, page counts, text extraction, undefined citations/references, missing glyphs, overfull boxes, and embedded/subset fonts.

- [ ] **Step 4: Review every rendered page**

Render both PDFs to contact sheets and inspect all pages for crowding, clipping, broken tables, and figure/caption problems.

- [ ] **Step 5: Complete bidirectional document review**

Check conversation-to-manuscript requirements, manuscript-to-repository claims, internal consistency, and English/Korean parity. Correct every gap or drift before handoff.
