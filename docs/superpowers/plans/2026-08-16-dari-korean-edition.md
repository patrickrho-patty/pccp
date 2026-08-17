# DARI Korean Edition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a complete, native-academic Korean edition of the authoritative DARI paper and publish both language editions as Zenodo version 1.1.

**Architecture:** Reconstruct `main_ko.tex` from the current English `main.tex`, preserving its technical structure and evidence while translating prose into established Korean systems/security-paper register. Maintain separate Korean TikZ/PGFPlots figure sources, validate the PDF structurally and visually, then create and verify a new version of the existing Zenodo record.

**Tech Stack:** XeLaTeX, BibTeX, TikZ, PGFPlots, GNU Make, Poppler PDF utilities, Zenodo deposition API/browser interface.

## Global Constraints

- `docs/plans/DARI/paper/main.tex` is the authoritative technical source.
- The Korean edition must preserve every section, subsection, appendix, figure, table, equation, citation, footnote, protocol identifier, benchmark value, and stated assumption.
- Korean prose must read as formal native academic Korean rather than a literal translation.
- Normative protocol objects, message names, field names, code, commands, hashes, equations, standards names, product names, and cited titles remain in their original form where translation improves neither clarity nor verification.
- The author is `Siook Rho (Patrick Rho)` and the affiliation is `Patty Co., Ltd.`; no visible manuscript date is added.
- The paper contains no feature-status reporting, progress language, self-diminishing commentary, or unsupported artifact-availability claim.
- Existing unrelated working-tree changes and every environment or secrets file remain untouched.
- Zenodo is not mutated until the Korean PDF passes all local structural, textual, typographic, and visual checks.

---

### Task 1: Reconcile the Authoritative Source and Build Contract

**Files:**
- Read: `docs/plans/DARI/paper/main.tex`
- Read: `docs/plans/DARI/paper/main_ko.tex`
- Read: `docs/plans/DARI/paper/Makefile`
- Read: `docs/plans/DARI/paper/README.md`
- Read: `docs/plans/DARI/paper/figures/*.tex`
- Read: `docs/plans/DARI/paper/references.bib`

**Interfaces:**
- Consumes: the approved Korean-edition design and current publication package.
- Produces: an exact structural inventory against which the translated source and figures can be checked.

- [ ] **Step 1: Read the English and stale Korean manuscripts in full**

Run:

```sh
sed -n '1,220p' docs/plans/DARI/paper/main.tex
sed -n '221,440p' docs/plans/DARI/paper/main.tex
sed -n '441,660p' docs/plans/DARI/paper/main.tex
sed -n '661,900p' docs/plans/DARI/paper/main.tex
sed -n '1,200p' docs/plans/DARI/paper/main_ko.tex
sed -n '201,400p' docs/plans/DARI/paper/main_ko.tex
sed -n '401,650p' docs/plans/DARI/paper/main_ko.tex
```

- [ ] **Step 2: Read the build, bibliography, and every figure source**

Run:

```sh
sed -n '1,260p' docs/plans/DARI/paper/Makefile
sed -n '1,260p' docs/plans/DARI/paper/README.md
sed -n '1,1200p' docs/plans/DARI/paper/references.bib
for file in docs/plans/DARI/paper/figures/*.tex; do sed -n '1,500p' "$file"; done
```

- [ ] **Step 3: Record the English structural inventory**

Run:

```sh
rg -n '^\\(section|subsection|appendix)|\\begin\{(figure|table|equation|align)|\\footnote|\\cite' docs/plans/DARI/paper/main.tex
```

Expected: a complete inventory used later for one-to-one reconciliation, not a second manuscript source.

### Task 2: Reconstruct the Complete Korean Manuscript

**Files:**
- Modify: `docs/plans/DARI/paper/main_ko.tex`

**Interfaces:**
- Consumes: the complete English manuscript and the terminology policy in the approved design.
- Produces: a standalone XeLaTeX manuscript with complete structural and claim parity.

- [ ] **Step 1: Replace the stale preamble and front matter**

Set XeLaTeX-compatible Korean fonts, preserve the English paper's packages and commands, use the approved Korean title, exact author and affiliation, and an empty `\date{}`.

- [ ] **Step 2: Translate the abstract through the architecture section**

Translate the abstract, introduction, contributions, recent motivating evidence, problem/threat model, protocol landscape, and architecture/trust-domain sections in natural Korean academic prose. Preserve citation keys, mathematical notation, message identifiers, evidence classifications, and claim boundaries exactly.

- [ ] **Step 3: Translate protocol mechanics through bounded authority**

Translate negotiation, authentication, framing, lanes, replay, resumption, failure behavior, grants, attenuation, policy epochs, disclosure bounds, and formal authority invariants. Retain normative object names and wire identifiers in monospace.

- [ ] **Step 4: Translate lifecycle, fabric, evidence, and extended profiles**

Translate the governed lifecycle, distributed-inference fabric, KV-cache-aware routing, worker admission, signed routing receipts, transactional effects, browser proof of possession, collaboration/federation, model supply, and multimedia profiles without adding implementation-status claims.

- [ ] **Step 5: Translate scenarios, evaluation, security, deployment, conclusion, and appendices**

Translate all five scenario analyses, benchmark methodology/results, security assumptions, deployment guidance, conclusion, message families, wire trace, conformance invariants, and cryptographic/error appendices. Preserve all measurements, units, equations, citations, and protocol literals.

- [ ] **Step 6: Check structural parity mechanically**

Run:

```sh
rg -n '^\\(section|subsection|appendix)|\\begin\{(figure|table|equation|align)|\\footnote|\\cite' docs/plans/DARI/paper/main.tex > /tmp/dari-en-structure.txt
rg -n '^\\(section|subsection|appendix)|\\begin\{(figure|table|equation|align)|\\footnote|\\cite' docs/plans/DARI/paper/main_ko.tex > /tmp/dari-ko-structure.txt
wc -l /tmp/dari-en-structure.txt /tmp/dari-ko-structure.txt
```

Expected: any count difference is explained by language-specific TeX layout only; no English content element is absent.

### Task 3: Rebuild All Korean Figures

**Files:**
- Create: `docs/plans/DARI/paper/figures/trust-domains-ko.tex`
- Create: `docs/plans/DARI/paper/figures/authority-evidence-state-ko.tex`
- Modify: `docs/plans/DARI/paper/figures/governed-exchange-lifecycle-ko.tex`
- Create: `docs/plans/DARI/paper/figures/governed-fabric-ko.tex`
- Create: `docs/plans/DARI/paper/figures/multimedia-chain-ko.tex`
- Create: `docs/plans/DARI/paper/figures/transport-benchmark-ko.tex`

**Interfaces:**
- Consumes: the six English TikZ/PGFPlots figures and unchanged benchmark datasets.
- Produces: six Korean figures with identical semantics/data and collision-free layouts.

- [ ] **Step 1: Translate the three protocol-state figures**

Rebuild trust domains, authority/evidence state, and governed exchange lifecycle with concise Korean labels. Keep protocol identifiers in English and increase node width, vertical separation, or annotation offsets where Korean text would collide.

- [ ] **Step 2: Translate the fabric and multimedia figures**

Rebuild governed fabric and multimedia chain with the same nodes and paths. Route arrows outside node interiors and place path labels so they cross neither boxes nor other labels.

- [ ] **Step 3: Translate the benchmark figure**

Keep every coordinate and unit unchanged. Translate titles/axes/legends, enlarge the plot's upper bounds or label offsets so values stay inside the frame, and retain complete labels such as `chunk 2`.

- [ ] **Step 4: Verify all six Korean inputs are referenced**

Run:

```sh
rg -n '\\input\{figures/.*-ko\.tex\}' docs/plans/DARI/paper/main_ko.tex
```

Expected: six distinct Korean figure inputs.

### Task 4: Build and Correct the Korean PDF

**Files:**
- Modify if required: `docs/plans/DARI/paper/Makefile`
- Modify if required: `docs/plans/DARI/paper/main_ko.tex`
- Modify if required: the six Korean figure sources
- Generate: `docs/plans/DARI/paper/DARI_Paper_KO.pdf`

**Interfaces:**
- Consumes: the reconstructed Korean manuscript and six Korean figures.
- Produces: a verified, embedded-font, searchable Korean PDF.

- [ ] **Step 1: Run the repository Korean verification target**

Run:

```sh
make -C docs/plans/DARI/paper verify-korean
```

Expected: exit 0 with no undefined citation/reference, missing glyph, or overfull-box rejection.

- [ ] **Step 2: Verify PDF identity and fonts**

Run:

```sh
pdfinfo docs/plans/DARI/paper/DARI_Paper_KO.pdf
pdffonts docs/plans/DARI/paper/DARI_Paper_KO.pdf
pdftotext docs/plans/DARI/paper/DARI_Paper_KO.pdf /tmp/DARI_Paper_KO.txt
test -s /tmp/DARI_Paper_KO.txt
rg -n '개요|서론|결론|부록|Siook Rho \(Patrick Rho\)' /tmp/DARI_Paper_KO.txt
```

Expected: a nonempty searchable PDF, embedded fonts, Korean section text, and exact author display.

- [ ] **Step 3: Render every page for visual review**

Run:

```sh
render_dir="$(mktemp -d)"
pdftoppm -png -r 150 docs/plans/DARI/paper/DARI_Paper_KO.pdf "$render_dir/page"
```

Inspect every rendered page, with particular attention to all six figures, tables, long protocol identifiers, citations, page breaks, and Korean glyph composition.

- [ ] **Step 4: Correct and rerun all checks until clean**

For each observed collision, clipping, malformed line, or typography defect, edit only the responsible Korean source and repeat Steps 1–3. Do not publish a merely compiling artifact.

### Task 5: Update the Local Publication Documentation

**Files:**
- Modify: `docs/plans/DARI/paper/README.md`

**Interfaces:**
- Consumes: the verified Korean build behavior.
- Produces: publication-package documentation that accurately describes both editions.

- [ ] **Step 1: Document the Korean edition and build target**

State that `main.tex` remains authoritative, `main_ko.tex` is the complete Korean translation, and `make korean` / `make verify-korean` build and verify `DARI_Paper_KO.pdf`.

- [ ] **Step 2: Verify documented commands**

Run:

```sh
make -C docs/plans/DARI/paper verify-korean
```

Expected: the documented verification command exits 0.

### Task 6: Create and Publish Zenodo Version 1.1

**Files:**
- Read only: `.env`
- Upload: `docs/plans/DARI/paper/DARI_Paper.pdf`
- Upload: `docs/plans/DARI/paper/DARI_Paper_KO.pdf`

**Interfaces:**
- Consumes: explicit user authorization, the existing Zenodo record `21971754`, and both verified PDFs.
- Produces: one public Zenodo version containing the authoritative English and Korean editions.

- [ ] **Step 1: Reverify the English artifact before external mutation**

Run:

```sh
shasum -a 256 docs/plans/DARI/paper/DARI_Paper.pdf
stat -f '%N %z' docs/plans/DARI/paper/DARI_Paper.pdf docs/plans/DARI/paper/DARI_Paper_KO.pdf
```

Expected English SHA-256: `47eea2fa6e1d55ec239c70ebbafbc37249c0f360bffceb76fd13a473604096d8`.

- [ ] **Step 2: Create a new version draft and inspect inherited metadata**

Use the Zenodo token without printing it. POST the `newversion` action for record `21971754`, follow `latest_draft`, and GET the draft. Verify creator, English title, preprint type, public access, keywords, license, and concept DOI before upload.

- [ ] **Step 3: Upload both PDFs and set bilingual metadata**

Keep `DARI_Paper.pdf`, add `DARI_Paper_KO.pdf`, set version `1.1`, add Korean language and translated-title metadata supported by Zenodo, and state in the description that both editions are included and English is authoritative.

- [ ] **Step 4: Verify the complete draft and publish**

Compare remote filenames, sizes, and checksums with the local artifacts; inspect the complete metadata response; then invoke the publish action authorized by the user.

- [ ] **Step 5: Verify the public record**

GET the new public record and download both files. Verify the new version DOI, concept DOI `10.5281/zenodo.21971753`, version `1.1`, bilingual metadata, public visibility, filenames, sizes, and checksums.

### Task 7: Record the Published Version and Run Final Scope Verification

**Files:**
- Modify: `docs/plans/DARI/paper/ZENODO_SUBMISSION.md`

**Interfaces:**
- Consumes: verified local artifacts and the verified public Zenodo response.
- Produces: an auditable local record of the bilingual publication.

- [ ] **Step 1: Update the publication record**

Record the new record ID and version DOI, preserved concept DOI, version 1.1 metadata, both PDF sizes and SHA-256 hashes, language and translated-title treatment, publication checks, and citation information returned by Zenodo.

- [ ] **Step 2: Run final local checks**

Run:

```sh
make -C docs/plans/DARI/paper verify-korean
shasum -a 256 docs/plans/DARI/paper/DARI_Paper.pdf docs/plans/DARI/paper/DARI_Paper_KO.pdf
git diff --check -- docs/plans/DARI/paper docs/superpowers/specs/2026-08-16-dari-korean-edition-design.md docs/superpowers/plans/2026-08-16-dari-korean-edition.md
```

Expected: Korean verification exits 0, hashes match the publication record, and `git diff --check` reports no whitespace errors.

- [ ] **Step 3: Reconcile deliverables against the approved design**

Confirm the complete Korean manuscript, six Korean figures, searchable PDF, unchanged English artifact, bilingual Zenodo version, public download verification, and updated local publication record. Report any remaining discrepancy instead of declaring completion.
