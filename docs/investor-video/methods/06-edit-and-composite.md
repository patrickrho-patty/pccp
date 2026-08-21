# Method 06 — Edit, Composite, Typography, and Sound

## Finishing system

Use DaVinci Resolve with Fusion, or an equivalent deterministic NLE/compositor. ViMax outputs are source clips, never the delivery master.

Timeline:

- 3840×2160 UHD, 16:9, 23.976 fps.
- 48 kHz audio.
- Work in a color-managed project; normalize all sources before the creative grade.
- Maintain title-safe margins for mobile playback.

## Assembly order

1. Place the final Korean narration and build a radio edit.
2. Set exact scene boundaries from `SCENES.md`, adjusting pauses rather than accelerating speech.
3. Add authentic Patty Code/PCCP/product proof.
4. Add selected ViMax plates around proof footage.
5. Add sound design and licensed score.
6. Add all typography, subtitles, logos, and award captions in post.
7. Grade and mix.
8. Run Method 07 before export.

## Typography

- Use one Korean type family with clear commercial usage rights; preferred style is contemporary grotesk, medium weight, generous tracking.
- Main titles: maximum 14 Korean characters per line where possible.
- Supporting titles: maximum two lines.
- White is warm, not pure; cobalt is reserved for governed/approved state; red appears only before the control boundary.
- No kinetic word clouds, typewriter-code effects, or PowerPoint-style bullet animation.
- Award captions and commercial target are factual cards; hold long enough to read on a phone.

## Subtitles

Burn Korean subtitles into the review/delivery version because the film may be watched muted in KakaoTalk or a mobile browser. Also export a clean master and UTF-8 `.srt`.

- Maximum two lines.
- Break by meaning, not equal character count.
- Keep each event roughly 1–6 seconds.
- Do not subtitle intentional dedication/logo cards redundantly.
- Subtitle wording must match `NARRATION_KO.md`; corrections happen in the narration master first.

## Logos

- Never generate logos with AI.
- Use supplied vector/transparent masters.
- Preserve aspect ratio and official colors.
- No glow, extrusion, particle disintegration, or morphing between Patty and hospital marks.
- The hospital logo appears only in the dedication and final invited-future card.
- Product logos appear only after authentic product behavior establishes ownership and purpose.

## Product UI compositing

- Preserve the authentic UI. Perspective placement, screen reflection, and a slow push are allowed; rebuilding controls is not.
- Do not make small UI unreadable through excessive depth of field.
- If a captured value is fixture data, do not add `LIVE` in post.
- Hide browser chrome only if it does not imply a native app.

## Music and sound design

Score arc:

- 00:00–00:42: sparse electronic pulse, distant machinery, restrained Seoul night ambience.
- 00:42–01:40: low strings and tightening percussion.
- 01:40–02:20: silence at the PCCP boundary, then controlled harmonic expansion.
- 02:20–03:22: confident rhythmic momentum, no triumphant fanfare.
- 03:22–04:42: orchestra gradually joins modern synthesizers.
- 04:42–05:00: piano and warm strings withdraw into room tone.

Avoid obvious traditional Korean instrumentation unless a composer uses it subtly and non-cliché. Do not use generated music or stock music without documented commercial rights.

## Mix targets

- Narration remains the priority at all times.
- Integrated loudness target: approximately -14 LUFS for private web/mobile delivery, true peak no higher than -1 dBTP.
- Duck music smoothly under speech; no pumping.
- Check phone speaker, laptop, headphones, and muted playback.
- Generated clip audio is discarded unless specifically selected and cleared; never mix inconsistent generated dialogue beneath narration.

## Delivery files

```text
patty-investor-film_master_uhd_prores.mov
patty-investor-film_review_1080p_h264.mp4
patty-investor-film_mobile_1080p_h264.mp4
patty-investor-film_clean_no-subs.mov
patty-investor-film_ko.srt
patty-investor-film_checksums.txt
```

The mobile encode must start quickly, remain legible on a phone, and preserve Korean glyphs. Share through a private link with access controls appropriate to the investor conversation; do not upload publicly by default.

