# Method 01 — ViMax Production

## Decision

Use the local `/Users/patrickrho/projects/ViMax` checkout as an agentic preproduction and cinematic-plate system. Use its `script2video` workflow because this package supplies an explicit, approved screenplay. Do not give ViMax the investor brief and let it rewrite the business story.

ViMax owns:

- shot decomposition;
- camera planning and dependency graph;
- character/environment reference generation;
- keyframes;
- short cinematic clips;
- candidate inspection and regeneration checkpoints.

ViMax does not own:

- authentic product recordings;
- logos or awards;
- readable typography;
- Korean subtitles;
- final Korean narration;
- music licensing and final mix;
- the master five-minute edit.

## Why this boundary fits the source

ViMax v1.2.0 provides persistent projects, planning artifacts, storyboard preview, render checkpoints, uploads, and provider settings. Its paper and source emphasize hierarchical planning, graph-based visual dependencies, reference-conditioned keyframes, and short video generation. Its current Script2Video final stage concatenates generated clips; it does not implement a full finishing timeline. Web uploads are stored and previewed but are not automatically injected as character references by the render tool contract.

## No-cost preflight

No external generation call may be made until the operator explicitly approves the provider and spend. Existing credentials are not spending consent.

```bash
cd /Users/patrickrho/projects/ViMax
git status --short --branch
git rev-parse HEAD
uv run pytest -q
cd web
npm test
npm run build
```

Stop if the checkout is dirty from another operator, tests fail, or the configured provider cannot report/limit expected spend. Never print `configs/agent.local.yaml`, environment values, or API keys.

## Start the workspace

The upstream README lists Linux and Windows as supported generation environments. The Web UI can be launched locally on the current machine, but rendering on macOS is an unverified path until the preflight passes.

```bash
cd /Users/patrickrho/projects/ViMax
./vimax web
```

Open `http://127.0.0.1:4173`. Do not bind the Web UI to a public interface. If port 4173 is occupied, use a free local port through `VIMAX_WEB_PORT` without changing tracked config.

## Project/session strategy

Create one named ViMax project for each generative scene block:

```text
patty-film-s02-seoul-authority
patty-film-s03-governance-gap
patty-film-s05-trust-layer
patty-film-s08-commercialization
patty-film-s09-ergazo-horizon    # fallback only
patty-film-s10-korea-scale
```

The first five projects are required. The Ergazo project is planned and rendered only if authentic Ergazo capture is not presentation-safe. This split is deliberate: the Script2Video planning prompt is scene-oriented, while the final film mixes authentic and generated sources. Separate blocks make individual failures replaceable and prevent a single render from rewriting timing across five minutes.

For every block:

1. Paste the exact source scene from `SCENES.md`.
2. Explicitly confirm `script2video` in the ViMax conversation.
3. Paste the block's exact requirements and global visual bible.
4. Run narrative planning only.
5. Inspect `characters.json`, `storyboard.json`, `camera_tree.json`, and every `shot_description.json`.
6. Revise specific artifacts until shot count, character description, no-text rule, lens, and ending state match the scene file.
7. Obtain explicit provider/spend approval.
8. Render keyframes and inspect every frame before accepting video generation.
9. Render clips; copy selected originals into `assets/generated/clips` with sidecars.

## Continuity contract

Use the same identifier and unchanged physical description for the developer in every scene:

```text
<KOREAN_DEVELOPER>: Korean, early thirties, natural oval face, short dark hair,
understated charcoal work jacket over a plain graphite shirt, no visible brand,
calm observant demeanor, realistic skin texture, no celebrity resemblance.
```

Because separate Script2Video sessions do not automatically share uploaded portrait references, continuity must be verified at each keyframe checkpoint. Preferred remedies:

1. Reuse the first accepted portrait as an explicit image-generation reference through a supported backend workflow.
2. If the active ViMax adapter cannot wire that upload into Script2Video, copy the accepted portrait registry into the new session only after verifying identifier/path compatibility.
3. If neither is reliable, avoid face-forward shots in later blocks and preserve continuity through wardrobe, silhouette, hands, and environment.

Do not claim the Web UI upload alone guarantees conditioning; the current source does not support that claim.

## Artifact review

Accept a planned shot only if:

- it has one clear narrative purpose;
- the first and last frames describe a feasible short clip;
- camera movement is singular and restrained;
- there is no requested readable text;
- Korean places and people are described without stereotype;
- any action remains physically and temporally plausible;
- clip duration can be shortened in the edit without losing meaning.

Reject any generated frame with broken anatomy, impossible reflections, changing wardrobe, pseudo-writing, distorted Seoul landmarks, fake corporate marks, or surveillance imagery that undermines the human-centered thesis.

## Render settings

- Aspect ratio: 16:9.
- Landscape enforcement: on.
- Generate clean picture plates; disable generated speech where the provider supports it.
- If the configured OpenRouter video adapter is used, note that its source defaults to 8-second, 720p clips with generated audio enabled unless environment overrides are supplied. Set audio off for plates and record the selected duration/resolution in the sidecar.
- Never force-rerender a whole project to repair one shot until the selected artifacts are copied to the asset workspace.

## Exit criteria

- Every required ViMax block has approved structured artifacts.
- At least one clean editorial option exists per required generated shot.
- No generated lettering is relied upon.
- Character/environment continuity is acceptable at normal playback speed.
- All provider use and costs were explicitly approved and recorded.
