# PAPER Korean Edition Design

## Goal

Produce a standalone Korean-language PDF of the complete PAPER manuscript that reads like an independently authored Korean computer-science paper rather than a literal translation.

## Translation Standard

- Use formal Korean scholarly prose with concise declarative endings such as `~한다`, `~이다`, and `~로 본다`.
- Reconstruct sentences and paragraph flow where Korean academic convention differs from English syntax.
- Preserve technical distinctions, evidentiary limits, equations, measurements, citations, and claim strength.
- Retain protocol identifiers and established proper nouns in English: PAPER, PCCP, Governed Exchange, Harness, Relay, PIA, Capability Lease, Policy Epoch, Catalog Model, PMP, Endpoint Lease, MCP, A2A, SCITT, QUIC, CBOR, COSE, and HPKE.
- Introduce an English term in parentheses only when it prevents ambiguity; do not repeat bilingual glosses mechanically.
- Translate all prose, headings, table cells, captions, appendix material, and editable diagram/chart labels.
- Preserve bibliography metadata and citation keys unchanged.

## Typesetting

Create `main_ko.tex` as an independent XeLaTeX entry point. Use Noto Serif CJK KR for body text and Noto Sans CJK KR for headings and sans-serif elements, with the existing page geometry, spacing, equations, colors, tables, and figure order. Produce `PAPER_arXiv_KO.pdf` without modifying the English PDF.

The two conceptual PNG illustrations contain baked-in English labels and remain unchanged. Their captions and surrounding explanations are translated. LaTeX-native figures and the benchmark chart receive Korean labels through Korean-specific figure source files.

## Verification

- Build the Korean PDF with XeLaTeX and BibTeX.
- Confirm all Hangul glyphs render and fonts are embedded.
- Confirm citations and cross-references resolve and no content is clipped.
- Compare section, figure, table, equation, citation, and appendix coverage against the English manuscript.
- Visually inspect every page.
- Review the Korean prose for literal-translation artifacts, inconsistent terminology, weakened claims, and inflated claims.
