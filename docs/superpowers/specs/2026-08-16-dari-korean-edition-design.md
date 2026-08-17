# DARI Korean Edition Design

## Objective

Create a complete Korean-language edition of the current English DARI paper
and publish it alongside the English PDF as a new version of the existing
Zenodo record. The Korean edition is a native academic translation, not a
revision of the stale Korean manuscript. The English manuscript remains the
authoritative technical source.

## Selected Publication Model

The existing Zenodo record remains the single publication lineage. A new
version, identified as version 1.1, will contain two separate files:

- `DARI_Paper.pdf`, the authoritative English edition and default paper;
- `DARI_Paper_KO.pdf`, the complete Korean translation.

This keeps citations, version history, and discovery under the existing
all-versions DOI while giving each language a clean, readable PDF. A separate
Korean DOI and a side-by-side bilingual PDF are outside this design.

The record will retain public access, the preprint resource type, and the CC BY
4.0 license. Its primary title will remain the English title. Korean will be
added as a record language, and a Korean translated title will be added where
Zenodo's current metadata model supports it. The description will state that
the record includes English and Korean editions and that the English edition is
authoritative.

## Source and Translation Contract

`docs/plans/DARI/paper/main.tex` is the sole technical source for the Korean
edition. `main_ko.tex` may be consulted for established phrasing, but its old
structure, claims, status language, and omissions are not carried forward.

The Korean edition will preserve the English paper's complete structure and
technical meaning:

- every main section, subsection, appendix, figure, table, equation, citation,
  footnote, protocol identifier, benchmark value, and stated assumption;
- the same distinction among protocol semantics, evaluated behavior, and
  deployment assumptions;
- the same positive, contribution-led voice without feature-completion labels,
  progress reporting, self-diminishing commentary, or an artifact-availability
  claim that has not been authorized;
- the same author display, `Siook Rho (Patrick Rho)`, and affiliation,
  `Patty Co., Ltd.`; and
- no visible manuscript date, matching the authoritative English paper.

No new technical claim will be introduced by translation. If an English phrase
has no exact Korean equivalent, the Korean prose will preserve the underlying
protocol distinction rather than following English word order literally.

## Korean Editorial and Terminology Policy

The prose will use formal, natural research Korean suitable for a systems and
security paper. Protocol object names, message names, field names, code,
commands, hashes, equations, standards names, product names, and cited titles
remain in their original form where translation would make verification harder.
Important technical terms may introduce the English term in parentheses on
first use and then use one consistent Korean form.

The working Korean title is:

> DARI: 거버넌스 AI와 분산 추론을 위한 권한 내재형 교환

The terminology baseline is:

| English concept | Korean form |
|---|---|
| authority-bearing exchange | 권한 내재형 교환 |
| governed exchange | 거버넌스 적용 교환 |
| Authorization Grant | 인가 증서 |
| Evidence Receipt | 증거 영수증 |
| attenuation | 권한 감쇠 |
| transcript-bound | 교환 기록 결합형 |
| worker capability card | 워커 역량 카드 |
| lane | 레인 |
| KV cache | KV 캐시 |
| model artifact | 모델 아티팩트 |
| tool effect | 도구 효과 |

Terms may remain bilingual where the English identifier is part of the wire
protocol. The glossary is a consistency baseline rather than permission to
rename normative protocol objects.

## Figures and Tables

All six figures used by the current English manuscript will receive Korean
variants with the same information architecture, data, identifiers, and paths:

1. trust domains;
2. authority and evidence state;
3. governed exchange lifecycle;
4. governed distributed-inference fabric;
5. multimedia chain; and
6. controlled transport benchmark.

Labels, legends, annotations, captions, and surrounding explanations will be
translated. Protocol identifiers, numeric values, symbols, units, and plotted
data remain unchanged. Layout will be adjusted for Korean text so labels do not
overlap nodes, arrows, plot borders, or one another.

Every table will preserve the English edition's rows, columns, values, and
claim boundaries while translating titles, headings, and explanatory prose.

## Repository Changes

The implementation is limited to the publication package and its publication
record:

- replace `docs/plans/DARI/paper/main_ko.tex` with the complete Korean edition;
- add or replace the six Korean figure sources under
  `docs/plans/DARI/paper/figures/`;
- adjust the paper `Makefile` only if the Korean build requires a dependency or
  verification correction;
- update `docs/plans/DARI/paper/README.md` to describe the completed Korean
  edition and its build command;
- generate `docs/plans/DARI/paper/DARI_Paper_KO.pdf`; and
- after publication, update
  `docs/plans/DARI/paper/ZENODO_SUBMISSION.md` with the new version DOI,
  metadata, filenames, sizes, and verified checksums.

The English manuscript is not rewritten as part of this translation. Existing
unrelated working-tree changes and all environment or secrets files remain
untouched.

## Verification

The Korean artifact is ready for publication only when all of the following
checks pass:

1. The Korean section, subsection, appendix, figure, table, equation, citation,
   and footnote structure is reconciled against the English source.
2. The Korean build completes through the repository's verification target
   without undefined references, undefined citations, missing glyphs, or
   materially overfull content.
3. Fonts are embedded and Korean text can be extracted from the PDF.
4. Every rendered page and every figure is visually inspected for clipping,
   overlap, unreadable labels, broken paths, poor float placement, and
   accidental blank space.
5. Names, benchmark values, identifiers, citations, and cryptographic notation
   match the English edition.
6. The final local English PDF still matches the already published version 1.0
   artifact unless a deliberate English correction is separately authorized.

## Zenodo Publication Procedure

After local verification, create a new version from Zenodo record `21971754`.
Carry forward the existing English PDF and upload the verified Korean PDF. Set
the record version to 1.1, retain the existing creator, public visibility,
preprint type, publication date, keywords, and CC BY 4.0 license, and add the
Korean language and translated-title metadata supported by Zenodo.

Before publishing, verify the draft metadata and both uploaded files. After
publishing, verify the public record, new version DOI, preservation of the
all-versions DOI `10.5281/zenodo.21971753`, downloadability of both PDFs, and
local-to-remote checksums and sizes. Record those results in
`ZENODO_SUBMISSION.md`.

No Zenodo draft or publication mutation occurs before the Korean PDF passes the
local checks. If Zenodo's API cannot faithfully express multiple languages or a
translated title, use the supported Zenodo interface rather than silently
dropping that metadata. The user's request in this conversation authorizes the
new-version creation, upload, metadata update, and publication after those
checks pass.

## Success Criteria

The task is complete when the Korean PDF is a full, visually sound translation
of the authoritative English paper; Zenodo exposes both editions in one new
public version; the English edition is clearly identified as authoritative;
and the repository records reproducible local and remote artifact identities
without exposing or modifying credentials.
