# PAPER Benchmark Mathematics and Chart Refinement Design

## Goal

Improve the paper's scientific presentation without adding decorative mathematics or unsupported empirical claims. The revision will make the benchmark chart readable at its upper bound, formalize calculations already supported by the checked-in measurements, and remove one duplicated paragraph.

## Scope

The work changes only the PAPER arXiv package. It will:

1. Keep the primitive-latency chart data-driven from `benchmark-data/core-primitives.tsv`.
2. Add logarithmic headroom above the 1 MiB record value so its label remains inside the plotting area.
3. Use consistent, legible value-label placement without altering any reported measurement.
4. Add three to five equations that define existing protocol and evaluation claims.
5. Remove the duplicated brownfield-migration paragraph.
6. Rebuild and visually inspect the PDF and source archive.

It will not regenerate the empirical chart with an image model, invent raw samples, compute confidence intervals from unavailable observations, claim statistical significance, add new benchmark results, or change the protocol method.

## Mathematical Additions

The revision will use a restrained set of equations:

1. **Governed transition predicate.** Define advancement of a protected transition as the conjunction of valid peer/session binding, Capability Lease, Policy Epoch, Relay verdict, and—when routing inference—approved package and endpoint state. This formalizes the paper's existing state-machine prose; it is not a security proof.
2. **Reported median.** Define the reported result for operation (o) as the median of the five Go benchmark repetitions. State explicitly that the stored artifact contains only the resulting median, so dispersion and confidence intervals cannot be recovered.
3. **Size-normalized record cost.** Define (c(s)=t(s)/s) for record round-trip time (t(s)) and payload size (s). Use it only to discuss how fixed overhead is amortized across the three measured payload sizes.
4. **Effective payload throughput.** Define (q(s)=s/t(s)), clearly labeling this as in-memory payload throughput for encode/decode rather than network or application throughput.
5. **Allocation amplification.** Define (A(s)=B_{\mathrm{op}}(s)/s), supporting the existing observation that record benchmarks allocate roughly twice the large payload size.

If pagination or prose clarity favors four equations, the size-normalized cost and throughput definitions may share one aligned equation block. Every derived value included in prose or a table must be reproducible from the checked-in TSV.

## Chart Design

The existing PGFPlots figure remains the source of truth. Its logarithmic y-axis will receive explicit upper headroom above 97,016 ns/op, and value labels will use a uniform offset that remains within the axis. Axis units, log scaling, categories, and TSV binding remain unchanged. The chart is verified at the rendered PDF page size, not only as a standalone crop.

## Verification

The revision is acceptable when:

- the `Record 1M` value label is fully inside the plot border;
- every plotted value still matches the TSV;
- every new equation is defined in adjacent prose and supports a claim already present;
- the paper does not imply unavailable variance, significance, or end-to-end performance;
- the duplicate migration paragraph is absent;
- `make clean && make && make check && make archive` succeeds;
- the rebuilt source archive compiles in a fresh directory;
- all PDF pages are visually inspected for clipping, crowding, and pagination regressions.
