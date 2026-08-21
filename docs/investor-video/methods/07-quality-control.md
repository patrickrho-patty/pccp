# Method 07 — Final Quality Control

The film is releasable only when every required check passes or the remaining exception is documented and approved by the CEO.

## Story comprehension

- [ ] A nontechnical viewer can explain Patty Code in one sentence.
- [ ] A nontechnical viewer understands that AI agents can take actions.
- [ ] The authority/evidence problem is understood without protocol jargon.
- [ ] PCCP is understood as the trust and control layer around Patty Code.
- [ ] Enterprise and Government/Sovereign are distinguishable.
- [ ] Plugg, Pin, and LawyerKit read as shipped capability proof, not random logos.
- [ ] Ergazo reads as the future horizon, not the main investment product.
- [ ] The requested action—continue toward investment—is emotionally clear.

## Factual claims

- [ ] No customer, pilot, revenue, or signed investment is implied.
- [ ] `10–15` is explicitly a twelve-month target, not a guarantee.
- [ ] Patty's “most trusted / No. 1” language is an ambition, not current market rank.
- [ ] Elevate wording is `Top 40+ Hottest Companies · No. 2 · Selected by The FoundersPress`.
- [ ] COMEUP wording is `BEST OF THE TOP 3 · Selected by Arageek · UAE`.
- [ ] No certification claim is made for PCCP.
- [ ] Naver/Kakao are used only as historical scale analogies; their logos and endorsement are absent.
- [ ] Product descriptions match the inspected repositories and recorded builds.

## Relationship and dignity

- [ ] Hospital logo appears only in opening dedication and closing invitation.
- [ ] No current partnership, adoption, endorsement, or investment is stated.
- [ ] Gratitude sounds personal, not desperate.
- [ ] Funding is a commercialization catalyst, not a rescue narrative.
- [ ] No patient, clinical, or hospital operational claim is made.

## Generated media

- [ ] No generated readable text, logos, seals, or watermarks remain.
- [ ] No malformed hands, faces, reflections, geometry, or wardrobe changes are visible at speed or frame-by-frame.
- [ ] Seoul/Korea looks specific and contemporary without cliché.
- [ ] Character identity is consistent enough that continuity is not distracting.
- [ ] Generated media is not passed off as authentic product or customer footage.

## Product capture and privacy

- [ ] Only synthetic data and repositories are visible.
- [ ] No API key, token, email account, local username, private path, browser profile, notification, or unrelated tab appears.
- [ ] Enterprise and Sovereign footage comes from genuinely separate profile states.
- [ ] Patty internal operations dashboard is absent.
- [ ] Fixture data is not labeled live.
- [ ] UI text is readable at normal phone size.

## Editorial and audio

- [ ] Runtime is 4:50–5:00; target master is 5:00.
- [ ] The first 30 seconds feel like a film rather than a product deck.
- [ ] No shot exists only because it was expensive to generate.
- [ ] Narration is intelligible on phone, laptop, and headphones.
- [ ] Korean subtitles match the final voice exactly and render without missing glyphs.
- [ ] Music/SFX rights are documented.
- [ ] Logos maintain correct proportion, color, and clear space.
- [ ] Loudness and true-peak targets pass.

## Export verification

Run media inspection and record the output:

```bash
ffprobe -v error -show_entries format=duration:stream=index,codec_name,width,height,r_frame_rate,sample_rate,channels \
  -of json assets/deliverables/patty-investor-film_review_1080p_h264.mp4

shasum -a 256 assets/deliverables/* > assets/deliverables/patty-investor-film_checksums.txt
```

Verify:

- [ ] Review file opens from beginning, midpoint, and final frame.
- [ ] Duration, resolution, frame rate, and audio sample rate match the delivery spec.
- [ ] First frame is intentional black; no accidental editor slate.
- [ ] Final frame holds and fades cleanly.
- [ ] Private-link playback was tested on the intended phone network.

## Human review gates

1. **CEO factual review:** claims, names, products, milestones, award wording.
2. **Brand review:** all logos, spellings, pronunciation, colors, and relationship language.
3. **Cold-viewer review:** one Korean-speaking nontechnical viewer explains the film back without prompting.
4. **Final CEO sign-off:** picture, narration, subtitles, and delivery link are approved together.

