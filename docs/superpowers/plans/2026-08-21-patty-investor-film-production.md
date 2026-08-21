# Patty Investor Film Production Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce and privately deliver a verified, cinematic five-minute Korean investor film that makes Patty Code/PCCP understandable and moves 윤강준 원장님 toward investment.

**Architecture:** Use ViMax Script2Video projects for controlled cinematic scene blocks, capture real Patty product interfaces in isolated synthetic environments, and assemble every source in a deterministic finishing timeline. Keep generated media, authentic evidence, narration, typography, and logos as separate asset classes until final composite.

**Tech Stack:** ViMax v1.2.0, configured image/video providers subject to explicit spend approval, Patty Code Wails desktop, PCCP Go/React stack, screen recorder, DaVinci Resolve/Fusion or equivalent, ffmpeg/ffprobe.

**Spec:** `docs/investor-video/README.md` and `docs/investor-video/SCENES.md`

## Global Constraints

- Final runtime is 4:50–5:00; master timeline target is 5:00.
- Korean narration and burned-in Korean subtitles; all readable text is deterministic post-production.
- No real user, customer, patient, repository, credential, or production data.
- No paid provider or cloud action without explicit operator approval of resource and expected spend.
- No current customer, pilot, revenue, hospital adoption, signed investment, certification, or guaranteed deployment claim.
- Show Enterprise and Government/Sovereign PCCP only; never Patty internal operations.
- Maintain immutable source assets and sidecar provenance per `ASSET_MANIFEST.md`.

---

### Task 1: Intake and lock source assets

**Files:**
- Populate: `assets/source/logos/`
- Populate: `assets/source/awards/`
- Create: sibling asset metadata sidecars

**Interfaces:**
- Consumes: user's authentic logos and award images.
- Produces: `A1`-verified immutable source assets for capture/editing.

- [ ] Copy Patty, hospital, Plugg, Pin, and LawyerKit source logos into `assets/source/logos` without modifying originals.
- [ ] Copy authentic Elevate and COMEUP images into `assets/source/awards`.
- [ ] Record creator/source, rights, resolution, color profile, and approved spellings in sidecars.
- [ ] Compare each mark with the current product/repository identity and reject stale variants.
- [ ] Promote accepted sources from `A0` to `A1` in the asset log.

**Verify:** Every required source in `ASSET_MANIFEST.md` exists or has a documented neutral-text fallback; no unverified substitute is present.

### Task 2: Record and approve narration

**Files:**
- Source: `docs/investor-video/NARRATION_KO.md`
- Create: `assets/audio/narration/vo_ko_master_v001.wav`
- Create: `assets/audio/narration/vo_ko_master_v001.md`

**Interfaces:**
- Consumes: locked Korean narration and confirmed brand pronunciations.
- Produces: 48 kHz/24-bit mono narration master and timing map.

- [ ] Confirm Plugg and Ergazo Korean pronunciations with the CEO before recording.
- [ ] Choose either a professional Korean narrator or a commercially licensed high-quality synthetic voice; do not assume the CEO will narrate and do not clone an identifiable voice without explicit permission.
- [ ] Produce two restrained performances, never a sales-announcer read.
- [ ] Edit breaths/noise without time compression or synthetic emphasis.
- [ ] Select one performance and mark sentence-level in/out times.
- [ ] Have the CEO approve wording, tone, pronunciation, and name delivery.

**Verify:** Peaks remain below -6 dBFS; recording contains no clipping, music, reverb, or pronunciation uncertainty.

### Task 3: Plan ViMax scene blocks

**Files:**
- Source: `docs/investor-video/SCENES.md`
- Create through ViMax: five persistent project workspaces under `.working_dir/`
- Copy approved planning artifacts to the production archive

**Interfaces:**
- Consumes: exact scene scripts, global visual bible, negative prompt.
- Produces: approved characters, storyboards, shot descriptions, camera trees.

- [ ] Run ViMax no-cost preflight from Method 01.
- [ ] Create the five required named Script2Video projects; create the Ergazo fallback project only if authentic capture is not presentation-safe.
- [ ] Paste exact scene source and explicitly confirm `script2video` for each.
- [ ] Run planning without render.
- [ ] Inspect and revise every structured artifact against shot intent and continuity rules.
- [ ] Freeze approved artifact copies before any provider render.

**Verify:** Each project reports ready for render; no storyboard requests text/logos and every shot has one feasible camera move.

### Task 4: Generate and select cinematic plates

**Files:**
- Create: `assets/generated/keyframes/`
- Create: `assets/generated/clips/`
- Create: AI-generation sidecars

**Interfaces:**
- Consumes: approved ViMax artifacts and explicit provider/spend approval.
- Produces: selected clean plates for scenes 2, 3, 5, 8, and 10.

- [ ] Present provider/model, clip count, duration/resolution, and expected spend to the operator; wait for explicit approval.
- [ ] Render keyframes only and conduct anatomy/text/continuity review.
- [ ] Regenerate rejected keyframes before video calls.
- [ ] Render video clips with generated speech disabled where supported.
- [ ] Copy selected source clips and keyframes out of ViMax session directories.
- [ ] Record prompt version, model/provider, references, date, and selection reason in sidecars.

**Verify:** Every required generated shot has an `A2` clean option and no text, logo, malformed anatomy, or false product imagery.

### Task 5: Capture Patty Code

**Files:**
- Create: `assets/capture/patty-code/*.mov`
- Create: capture sidecars

**Interfaces:**
- Consumes: Method 02 synthetic repository and Method 03 runbook.
- Produces: authentic request → plan → approval → test → result sequence.

- [ ] Build the Wails desktop from the recorded revision.
- [ ] Launch with the exact isolated `PATTY_HOME` under the capture root.
- [ ] Select integrated DARI mode only if disposable enrollment succeeds truthfully; otherwise document UI demonstration mode.
- [ ] Record PC01–PC06 with long handles.
- [ ] Review every frame for personal data, credentials, and private paths.

**Verify:** The sequence is truthful, synthetic, readable on mobile, and contains no fake governance state.

### Task 6: Capture PCCP Enterprise and Sovereign

**Files:**
- Create: `assets/capture/pccp-enterprise/*.mov`
- Create: `assets/capture/pccp-sovereign/*.mov`
- Create: capture sidecars

**Interfaces:**
- Consumes: disposable source copy, isolated databases, Method 04.
- Produces: authentic Enterprise and genuinely separate Sovereign proof.

- [ ] Build the disposable PCCP copy.
- [ ] Seed Enterprise synthetic data through supported APIs or a temporary helper inside the disposable copy.
- [ ] Record ENT01–ENT05.
- [ ] Start a separate isolated Sovereign database/profile and record SOV01–SOV03.
- [ ] Stop only recorded process PIDs and verify local ports are closed.
- [ ] Frame-check every capture for privacy and unsupported feature claims.

**Verify:** Profiles are authentic and distinct; no internal Patty operations, real data, certification badge, or fixture-as-live wording appears.

### Task 7: Capture product and recognition proof

**Files:**
- Create: `assets/capture/plugg/`, `pin/`, `lawyerkit/`, optionally `ergazo/`
- Process: source award images into `assets/post/`

**Interfaces:**
- Consumes: Method 05, authentic product builds/media, verified recognition images.
- Produces: five coherent evidence cards plus optional Ergazo horizon.

- [ ] Record one defining authentic action for Plugg, Pin, and LawyerKit.
- [ ] Use only local fixture-backed or supplied Pin media unless deployment receives explicit approval.
- [ ] Create restrained Elevate and COMEUP cards with exact approved wording.
- [ ] Record Ergazo only if the current build is presentation-safe and matches the narration; otherwise use a conceptual plate.
- [ ] Apply a single `PATTY가 만든 제품` card grammar across products.

**Verify:** A cold viewer can state what each product does before seeing its logo; award captions match the verified wording.

### Task 8: Assemble picture, typography, and subtitles

**Files:**
- Create: versioned NLE project and `assets/post/` elements
- Create: `patty-investor-film_ko.srt`

**Interfaces:**
- Consumes: approved narration, captures, plates, logos, photos.
- Produces: locked 5:00 picture with deterministic Korean typography.

- [ ] Build the radio edit and exact scene timing.
- [ ] Cut authentic proof before decorative plates.
- [ ] Add every title/logo/subtitle in the NLE or compositor, never in an AI generator.
- [ ] Apply approved captions and relationship guardrails.
- [ ] Perform a mobile legibility pass and a no-audio comprehension pass.

**Verify:** Runtime is within range, all claims are readable and accurate, and no generated pseudo-text survives.

### Task 9: Score, sound design, and mix

**Files:**
- Create: `assets/audio/music/`, `assets/audio/sfx/`
- Update: NLE project mix

**Interfaces:**
- Consumes: licensed audio and locked picture.
- Produces: narration-first final mix.

- [ ] Record license metadata before using music/SFX.
- [ ] Build the approved electronic-to-orchestral arc.
- [ ] Remove generated clip dialogue/audio unless deliberately selected and cleared.
- [ ] Mix for approximately -14 LUFS and no more than -1 dBTP.
- [ ] Check phone, laptop, and headphones.

**Verify:** Narration remains effortless to understand; licenses and measured loudness are recorded.

### Task 10: Final QC and private delivery

**Files:**
- Create: all deliverables named in Method 06
- Create: checksum file and final QC record

**Interfaces:**
- Consumes: locked picture and final mix.
- Produces: verified private delivery package.

- [ ] Run every checklist item in Method 07.
- [ ] Run `ffprobe` and record duration/stream details.
- [ ] Generate SHA-256 checksums.
- [ ] Obtain CEO factual, brand, and final sign-off.
- [ ] Run one Korean-speaking nontechnical cold-viewer comprehension check.
- [ ] Upload only to an approved private host and test mobile playback.
- [ ] Send with the separate signed personal letter and request a follow-up conversation.

**Verify:** Delivery link works privately on mobile; master/review/captions/checksums agree; CEO explicitly approves release.
