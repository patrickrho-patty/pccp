# Patty Investor Film Production Package

This package defines a five-minute, Korean-language investor film prepared for 강남베드로병원 원장 윤강준. It is a production specification, not a finished render.

## Objective

Move the viewer from curiosity to confidence and then to a personal decision: continue toward investment in Patty.

The film must leave a nontechnical viewer understanding:

1. AI coding agents can act, not merely answer.
2. Uncontrolled action creates an authority, security, and accountability gap.
3. Patty Code makes advanced coding agents accessible in Korea.
4. PCCP and DARI make those agents governable for companies and public institutions.
5. Patty has already demonstrated product-building ability through Plugg, Pin, and LawyerKit and received external recognition.
6. The next twelve months are about commercialization: 10–15 paid enterprise or public-sector deployments.
7. Patty Code is the entry point to a much larger Korean AI platform vision.
8. Patty wants to build that future with 윤강준 원장님 and 강남베드로병원.

## Locked creative decisions

- Runtime: 4:50–5:00; master timeline is exactly 5:00.
- Language: Korean narration, warm and restrained delivery.
- Opening: restrained techno-thriller that initially feels like a film.
- Human thread: one fictional Korean developer; Patty is the guide and Korea is the final protagonist.
- Text: no AI-generated lettering. All readable text, logos, subtitles, figures, and award captions are composited in post.
- Product UI: 20–30 seconds of authentic Patty Code and PCCP footage, used as cinematic proof rather than a dashboard tour.
- PCCP profiles: Enterprise and Government/Sovereign only; never show Patty's internal operations view.
- Ergazo: an 8–12 second horizon, not a second product pitch.
- Hospital: invited future strategic partner, never represented as a current customer, investor, adopter, or endorser.
- Financial ask: the investment amount is not shown.
- Commercial target: 10–15 paid enterprise or public-sector deployments within twelve months, always presented as a target—not a guarantee.
- Closing vision: Patty aims to change how Korea works with AI at national platform scale.

## Production architecture

```text
locked screenplay
    ├── ViMax Script2Video scene blocks ──> generated cinematic plates
    ├── authentic product capture ────────> Patty Code / PCCP / product proof
    ├── supplied brand and award assets ─> logos / photos / evidence
    └── Korean narration + licensed score
                         ↓
              deterministic finishing
        edit · composite · typography · subtitles · mix
                         ↓
          4K master + mobile review encode + captions
```

ViMax is the shot-planning and generative-plate engine. It is not the final editor. This boundary is intentional: the current local ViMax pipeline creates structured stories, keyframes, short clips, continuity anchors, and a concatenated output, but does not provide the deterministic typography, logo treatment, authentic UI integration, subtitle authoring, or final audio mix this film requires.

## Package map

- [`SCENES.md`](SCENES.md) — timecoded master screenplay, shot sources, narration, sound, and post instructions.
- [`NARRATION_KO.md`](NARRATION_KO.md) — clean Korean voice-over script with pronunciation and recording notes.
- [`LETTER_KO.md`](LETTER_KO.md) — separate one-page personal letter for 윤강준 원장님.
- [`DELIVERY_MESSAGE_KO.md`](DELIVERY_MESSAGE_KO.md) — concise private-link message for remote delivery.
- [`ASSET_MANIFEST.md`](ASSET_MANIFEST.md) — source/deliverable separation, filenames, rights, and acceptance gates.
- [`REFERENCES.md`](REFERENCES.md) — source-code and public-reference basis for the production decisions and factual cards.
- [`methods/01-vimax-production.md`](methods/01-vimax-production.md) — exact ViMax role, session structure, prompt rules, checkpoints, and cost gate.
- [`methods/02-demo-environment.md`](methods/02-demo-environment.md) — safe isolated capture environment.
- [`methods/03-patty-code-capture.md`](methods/03-patty-code-capture.md) — autonomous Patty Code build, launch, staging, and capture.
- [`methods/04-pccp-capture.md`](methods/04-pccp-capture.md) — autonomous Enterprise and Sovereign console capture.
- [`methods/05-product-proof.md`](methods/05-product-proof.md) — Plugg, Pin, LawyerKit, Elevate, and COMEUP proof montage.
- [`methods/06-edit-and-composite.md`](methods/06-edit-and-composite.md) — edit, typography, logo, subtitles, narration, and audio mix.
- [`methods/07-quality-control.md`](methods/07-quality-control.md) — factual, visual, audio, privacy, and export acceptance checks.

## Claim guardrails

Approved:

- “향후 12개월, 10–15개 기업 및 공공기관의 유료 도입을 목표로 합니다.”
- “우리는 대한민국에서 가장 신뢰받는 AI 코딩 플랫폼을 만들고자 합니다.”
- “Elevate Festival 2025 · Top 40+ Hottest Companies · No. 2 · Selected by The FoundersPress.”
- “COMEUP Global Media Awards 2025 · BEST OF THE TOP 3 · Selected by Arageek.”

Prohibited:

- Existing customers, pilots, revenue, hospital adoption, signed investment, or endorsement.
- “Already No. 1,” guaranteed deployment, guaranteed revenue, or “no competitor can surpass us.”
- Certification claims. PCCP can support controls and evidence; it must not be called automatically ISMS-P, CSAP, KCMVP, or legally certified.
- “Second place at Elevate” or “third among 500 at COMEUP.” Those descriptions do not match the verified public record.
