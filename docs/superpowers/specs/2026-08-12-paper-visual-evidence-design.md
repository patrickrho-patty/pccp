# PAPER Visual Evidence Design

## Objective

Bring the arXiv manuscript back into alignment with its publication plan by adding the missing visual and reproducibility artifacts without changing the DARI method or presenting unmeasured behavior as a result.

## Scope

The paper will contain four figures:

1. The existing Qwen conceptual overview, retained as the visual hook and explicitly labeled conceptual.
2. A LaTeX-native Governed Exchange lifecycle showing `OPEN`, `GOVERNANCE`, `ROUTE`, `STREAM`, optional `TOOL/APPROVAL`, and `CLOSE`. Gate annotations will identify peer/session binding, Capability Lease, Policy Epoch, Relay Verdict, model/package/endpoint resolution, independent tool authorization, and Evidence Receipt.
3. A LaTeX-native model identity chain separating the user-visible Catalog Model from the signed Patty Model Package, current Endpoint Lease, enrolled PIA, and local serving engine. The diagram will distinguish product identity, artifact identity, and deployment identity.
4. A log-scale primitive-latency plot generated only from `benchmark-data/core-primitives.tsv`. It will visualize the seven measured medians and explicitly state that these are in-memory primitive costs rather than network or end-to-end results.

No provenance-fidelity or end-to-end performance plot will be added because those measurements do not exist. The existing evidence-boundary, comparison, benchmark-detail, object, and invariant tables remain the detailed record.

## Rendering Architecture

The lifecycle and identity diagrams will use TikZ so text remains vector-sharp, accessible to LaTeX layout, and portable in the arXiv source archive. They will share a restrained visual grammar:

- blue-gray nodes for protocol state and identities;
- amber accents for enforcement gates;
- dark arrows for required transitions;
- dashed boundaries only for optional or external components;
- concise labels that remain legible at normal page width.

The benchmark chart will use PGFPlots and read the checked-in TSV directly. The build remains offline and uses standard TeX Live packages without shell escape. The existing Qwen PNG remains the only generated raster artwork.

## Manuscript Integration

- Place the lifecycle figure in “How an Exchange Earns the Right to Advance,” immediately after the lifecycle explanation.
- Place the model identity figure in “Server-authoritative model resolution.”
- Place the benchmark plot in “Preliminary Results,” adjacent to the interpretation, while retaining the numerical table.
- Reference every figure in the surrounding prose and use captions that state whether the figure is conceptual, protocol-design, or empirical.
- Update the ethics disclosure so it distinguishes the generated Qwen artwork from LaTeX-native technical diagrams and the data-derived plot.

## Remaining Publication-Plan Corrections

- Add SCITT to related work and the bibliography using its official specification/RFC.
- Put the evaluated commit, complete hardware/software environment, exact benchmark commands, dataset scope, and absence of raw per-run output directly in the artifact README.
- Update the publication-plan checklist to reflect completed work and replace its stale two-column requirement with the approved readable single-column typography.
- Regenerate the PDF and arXiv source archive.

## Verification

Success requires:

- a clean offline `make clean && make && make check`;
- no undefined citations/references or overfull boxes;
- every PDF page rendered and inspected for clipping, illegible labels, awkward floats, and blank regions;
- all fonts embedded;
- benchmark plot values matching the TSV exactly;
- a successful build from a freshly unpacked source archive;
- no claims of B0--B4, TTFT, scale, adversarial, or provenance-fidelity results.

## Evidence Boundary

The new diagrams explain the proposed protocol; they do not establish implementation completeness. The benchmark chart substantiates only the measured primitive-cost claim. Raw five-run benchmark output was not retained, so the artifact will disclose that limitation rather than reconstructing or fabricating it.
