# Asset Manifest and Naming Contract

The production agent must create this structure in the video production workspace, not inside application source directories.

```text
assets/
  source/                 # immutable originals
    logos/
    awards/
    references/
    product-media/
  capture/                # authentic screen recordings and stills
    patty-code/
    pccp-enterprise/
    pccp-sovereign/
    plugg/
    pin/
    lawyerkit/
    ergazo/
  generated/              # ViMax outputs copied from session workspaces
    keyframes/
    clips/
  audio/
    narration/
    music/
    sfx/
  post/                   # titles, mattes, composites, proxies
  deliverables/
```

Never edit an original in `assets/source`. Derived media goes in its destination folder with a new filename.

## Required supplied originals

| ID | Required asset | Preferred source | Acceptance |
|---|---|---|---|
| LOGO-PATTY | Patty company logo | SVG or transparent PNG | Correct current mark; ample clear space |
| LOGO-HOSPITAL | 강남베드로병원 logo | SVG or transparent PNG | Authorized authentic mark |
| LOGO-PLUGG | Plugg logo | SVG or transparent PNG | Current product mark |
| LOGO-PIN | Pin logo | SVG or transparent PNG | Current product mark |
| LOGO-LAWYERKIT | LawyerKit logo | SVG or transparent PNG | Current product mark |
| AWARD-ELEVATE | Authentic Elevate 2025 image | Original photo/graphic | Patty visibly connected to the recognition; source retained |
| AWARD-COMEUP | Authentic COMEUP 2025 award image | Original photo/graphic | Patty visibly connected to the recognition; source retained |

If an original is unavailable, the production agent records the gap and uses a neutral text card. It must not scrape and substitute an unverified logo.

## Filenames

Use lowercase ASCII and stable shot IDs:

```text
s02_sh01_seoul-office_v001.mp4
s04_pc01_patty-code-plan_v001.mov
s06_ent01_dashboard-overview_v001.mov
s07_pin01_neighborhood-verify_v001.mov
s07_award01_elevate-source_v001.jpg
vo_ko_master_v001.wav
```

Version increments represent a meaningful replacement, not every encode. Add `_proxy` or `_review` for derivatives, never to masters.

## Capture standards

- Screen master: 3840×2160 or highest native resolution at 60 fps, lossless or visually lossless intermediate.
- Generated clip: retain original provider output; do not upscale before editorial selection.
- Logo: SVG preferred; otherwise transparent PNG at least 2000 px on the long edge.
- Award/photo: original resolution, sRGB conversion only in derivatives; retain attribution and rights notes.
- Narration: mono 48 kHz / 24-bit WAV, peaks below -6 dBFS, no baked music or reverb.
- Music/SFX: 48 kHz WAV when available; store license receipt and permitted usage.

## Metadata sidecar

Every selected asset receives a sibling `.md` sidecar with:

```text
source:
creator:
captured_at:
repository_and_revision:
rights_or_permission:
contains_real_data: no
scene_usage:
notes:
```

For AI media also record model/provider, prompt version, reference inputs, generation date, and whether audio was generated. Never place API keys or raw provider responses in sidecars.

## Asset gates

- `A0`: received but unverified.
- `A1`: identity, ownership, and resolution verified.
- `A2`: privacy/content inspection passed.
- `A3`: selected in edit.
- `A4`: final-use rights and attribution checked.

Only `A2` or higher enters the edit. Only `A4` enters the delivery master.

