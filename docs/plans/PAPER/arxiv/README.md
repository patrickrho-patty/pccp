# PAPER arXiv publication package

This directory contains the self-contained LaTeX source and compiled PDF for the PAPER preprint.

## Build

Requirements: a standard TeX Live installation with `latexmk`, `pdflatex`, and BibTeX. The build requires no network access, proprietary fonts, or shell escape.

```sh
make clean
make
make check
```

The PDF is written to `PAPER_arXiv.pdf`.

## Reproduce the preliminary measurements

From the repository root:

```sh
go test ./internal/paper -run '^$' -bench 'Benchmark(RecordRoundTrip|CanonicalCBOR|EvidenceChainNext|ObjectDigest|Ed25519SignVerify)$' -benchmem -count=5
go test ./... -count=1
```

The reported values are medians of five local runs. They measure in-memory primitives, not network, Relay-policy, database, token-generation, or end-to-end inference latency.

## Create the source archive

```sh
make archive
```

Before submission, replace or confirm the author metadata in `main.tex` and attach any external artifact identifier promised by the final manuscript.
