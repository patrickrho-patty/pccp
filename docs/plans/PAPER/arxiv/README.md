# DARI arXiv publication package

This directory contains the self-contained LaTeX source and compiled PDF for
the DARI preprint, *DARI: Delegated Authorization and Receipts for Governed AI
Inference*. The directory and PDF target retain their historical names for
build compatibility.

## Build

Requirements: a standard TeX Live installation with `latexmk`, `pdflatex`, and BibTeX. The build requires no network access, proprietary fonts, or shell escape.

```sh
make clean
make
make check
```

The PDF is written to `PAPER_arXiv.pdf`.

## Reproduce the preliminary measurements

The evaluated implementation snapshot is repository commit
`0234034b51fb6f1a5127280ce1e9c209b8198004`. Measurements were collected on an
Apple M4 Pro with 14 logical CPUs and 48 GiB RAM, running macOS 27.0 on arm64
with Go 1.26.5.

From the repository root:

```sh
go test ./internal/paper -run '^$' -bench 'Benchmark(RecordRoundTrip|CanonicalCBOR|EvidenceChainNext|ObjectDigest|Ed25519SignVerify)$' -benchmem -count=5
go test ./... -count=1
```

The reported values in `benchmark-data/core-primitives.tsv` are medians of five
local runs. They measure in-memory primitives, not network, Relay policy,
database, persistence, model, GPU, TTFT, token generation, or end-to-end
inference latency. Raw per-run output was not retained for this preliminary
capture; it must be published before a complete empirical release. The TSV must
not be interpreted as a substitute for raw benchmark output.

The benchmark chart in the paper reads `median_ns_per_op` directly from this
TSV during the LaTeX build.

## Interpret implementation claims

`implementation-claim-matrix.tsv` is the authoritative evidence-status record.
It distinguishes implemented framing and transport bindings from
implementation-required authentication, authorization, delegation, and receipt
work, and from browser and federation extension profiles. Primitive benchmark
values must not be interpreted as measurements of those unimplemented or
extension-profile behaviors.

The overview PNG is Qwen-generated conceptual artwork. The lifecycle PNG is
YOLO AUTO-generated and rendered with ImageMagick from model-produced SVG.
They contain no empirical measurements or implementation-coverage claims. The
model-identity chain and primitive-latency plot are LaTeX-native figures; the
latter reads the checked-in benchmark TSV directly.

## Create the source archive

```sh
make archive
```

Before submission, replace or confirm the author metadata in `main.tex` and
attach any external artifact identifier promised by the final manuscript.
