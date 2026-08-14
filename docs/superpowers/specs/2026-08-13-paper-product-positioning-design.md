# PAPER Product-Positioning Revision Design

## Objective

Revise the English and Korean arXiv manuscripts so that PAPER is presented as an open, fully integrated product system rather than an MVP, prototype, or development-only vertical slice. The revision must be technically exciting and commercially credible without turning unverified assurance, adoption, or benchmark claims into facts.

## Central narrative

The paper will lead with one memorable distinction:

> Ordinary AI APIs govern around the inference call. PAPER makes governance part of the call.

PAPER is an open protocol specification and implementation for Governed Exchanges. Patty Code is its flagship product integration and a demanding proof of generality: a coding agent must govern identity, context disclosure, model routing, tool authority, durable edits, and evidence across one causal path. The same protocol surface applies to chat applications, copilots, automation systems, and custom AI products through the PIA SDK and serving-engine adapters.

The reader should leave with three reactions:

1. the missing abstraction is obvious once named;
2. the design is unusually complete because authority and evidence are unified in the exchange state machine; and
3. this is usable product infrastructure, not a diagram awaiting implementation.

## Evidence-backed product position

The paper may state that:

- the DARI specification is openly implementable;
- the Go implementation includes framing, canonical objects, signed evidence, state machines, Relay, PIA, policy, lease, catalog, provenance, transport, and conformance surfaces;
- PAPER is integrated into the official Patty Code Harness as the sole supported service-inference route;
- the Harness sends Catalog Model identity rather than arbitrary provider URLs or API keys;
- vLLM and SGLang adapters are implemented;
- the PIA SDK and custom-engine adapter boundary allow other AI applications to adopt PAPER;
- the semantic layer supports chat messages, streaming, tools, structured output, multimodal inputs, caching, and compaction;
- coding agents are the flagship integration, while chat applications and other AI systems are explicit integration classes.

The paper must not claim external certification, third-party interoperability, independent security review, large-scale customer deployment, or end-to-end benchmark results that the repository does not contain. Current authentication development defaults must be described as a deployment-profile assurance boundary, not as evidence that the product is merely a prototype.

## Manuscript changes

### Abstract and introduction

Replace feasibility language with an implemented-system result. Introduce the open-spec and reusable-infrastructure story in the abstract. Reframe the motivating incident so the answer is not only a protocol proposal but a running governed path in Patty Code.

### Contributions

Replace the “measured prototype foundation” contribution with two stronger contributions:

- a product-integrated reference system spanning Patty Code Harness, PCCP Relay, PIA, model-serving adapters, runtime authority, and provenance; and
- an open integration surface consisting of the specification, Go implementation, PIA SDK, vLLM/SGLang adapters, and custom application boundary.

### Implementation section

Rename the section to emphasize product integration. Describe the current system rather than the original evaluation snapshot alone. Add a subsection for the Patty Code end-to-end integration and a subsection for open adoption beyond coding agents.

Replace the old prototype evidence-boundary table with an implemented product-surface table. It should distinguish:

- open protocol/core library;
- official Patty Code Harness integration;
- Relay/Control Plane enforcement;
- PIA and vLLM/SGLang serving integration;
- SDK/custom application integration;
- provenance and evidence;
- benchmark and validation evidence.

### Evaluation

Rename “Preliminary Results” to “Measured System Primitives” or equivalent. Preserve the measured values and state precisely that they measure primitive overhead. Product completeness must not be conflated with measurement scope.

### Discussion and limitations

Replace generalized “future work” and “not production” language with scoped assurance boundaries. Separate:

- implemented product capability;
- deployment hardening choices such as certificate validation and credential provisioning;
- empirical claims not made by this paper, such as scale, TTFT, and provenance-fidelity accuracy.

The appendix should become a validation/assurance matrix rather than a list of gates separating a preliminary artifact from a complete revision.

### Conclusion

End with the product-level result: PAPER demonstrates that governance can be a reusable protocol property across coding agents and general AI applications. The final sentence should be concise and quotable.

## English/Korean parity

The Korean paper will be rewritten as native Korean research prose, not translated literally. Both editions must preserve the same claims, evidence boundaries, tables, measurements, citations, labels, and structure. Product terms may remain in English where that is standard Korean technical usage, but explanatory prose must be natural and publication-grade.

## Visual strategy

Keep the existing conceptual and benchmark figures unless the revised text makes one materially misleading. Prefer one new compact integration table over adding an unmeasured marketing graphic. Captions will be revised to reinforce the open-system and product-integration story.

## Verification

The revision is complete when:

- stale MVP/prototype/development-vertical-slice positioning is removed from both manuscripts;
- open specification, Patty Code integration, vLLM/SGLang adapters, SDK/custom integration, and chat/general-AI applicability are explicit in both editions;
- every strong implementation claim is traceable to repository code or documentation;
- benchmark values and scientific limitations remain accurate;
- English and Korean structures and claims match;
- both XeLaTeX/BibTeX builds pass with no undefined references, missing glyphs, or overfull boxes;
- both PDFs pass a full-page visual review.
