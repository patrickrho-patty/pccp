# Governed AI Exchange SVG Design

## Goal

Rebuild the Sensenova `GOVERNED AI EXCHANGE` PNG as an editable, publication-ready SVG. The SVG is the preferred paper artifact; the PNG remains available as a fallback and visual reference.

## Output

- `docs/plans/PAPER/graphics-probes/governed-ai-exchange.svg`
- 16:9 view box suitable for full-width or half-page placement
- real SVG text and shapes, with no embedded raster image

## Visual design

- white background;
- restrained navy, slate, white, and orange palette;
- flat rectangular process boxes with consistent geometry;
- uppercase sans-serif labels readable at reduced size;
- a conventional path above and a PAPER path below;
- the three governance badges grouped above `PAPER RELAY`;
- a continuous orange provenance spine below the PAPER path;
- an evidence-receipt document icon at the end of the spine.

## Semantic corrections

- The conventional path runs left to right from human intent to post-hoc logs.
- The dashed reconstruction arrow runs backward from post-hoc logs toward the originating interaction.
- The PAPER path runs left to right from human intent to code commit.
- Governance badges visually attach to the Relay rather than floating between unrelated stages.
- The provenance spine covers the governed exchange and terminates in the Evidence Receipt.

## Acceptance checks

1. The SVG parses successfully as XML.
2. It contains no `<image>` element or external resource dependency.
3. Every requested label is spelled exactly.
4. It renders clearly at 1600×900 and remains legible at paper-column scale.
5. The generated preview preserves the intended arrow directions and grouping.

## Scope

This task creates one SVG figure only. It does not edit the manuscript, generate benchmark charts, or replace the existing PNG probes.
