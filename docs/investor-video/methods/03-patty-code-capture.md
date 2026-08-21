# Method 03 — Patty Code Capture

## Source and revision

Use `/Users/patrickrho/projects/patty-code`. Before capture, record `git rev-parse HEAD` and require a clean worktree except for changes explicitly owned by the production run. Never modify or read normal Patty Code credentials.

## Build

```bash
cd /Users/patrickrho/projects/patty-code/desktop
wails doctor
wails build
```

Expected executable on this checkout: `desktop/build/bin/patty-desktop` (the Wails config output filename is `patty-desktop`). If the platform creates an application bundle, launch the actual bundle executable while retaining the isolated environment.

## Launch in a disposable home

```bash
: "${capture_root:?set capture_root to the exact directory created in Method 02}"
PATTY_HOME="$capture_root/patty-code-home" \
  /Users/patrickrho/projects/patty-code/desktop/build/bin/patty-desktop
```

`PATTY_HOME` is the supported isolation boundary for configuration, credentials, sessions, cache, skills, commands, hooks, and desktop tab state. Do not replace or export the process `HOME` variable.

## Provider boundary

The current Enterprise harness is DARI-only and requires a valid enrollment credential/key for a real governed session. Do not fabricate that state in the UI. Choose one of these evidence modes:

1. **Integrated mode:** use the disposable PCCP/DARI environment from Method 04, enroll the disposable harness through the supported flow, and record a real governed session.
2. **UI demonstration mode:** use the desktop frontend development mock for layout/tool-card shots only, clearly avoiding any visual claim that the session is connected to PCCP. Pair it with separate authentic PCCP governance proof.

Integrated mode is preferred. If enrollment cannot be completed without editing product code or exposing secrets, use UI demonstration mode and record the limitation in the asset sidecar.

## Capture sequence

| ID | Duration | State | Required content |
|---|---:|---|---|
| PC01 | 5–7 s | Empty workspace | Patty Code desktop, synthetic repo name, Korean composer |
| PC02 | 6–8 s | Planning | Korean request and concise plan; no private paths |
| PC03 | 5–7 s | Tool proposal | One readable tool card and affected synthetic files |
| PC04 | 5–7 s | Approval | Authentic approval modal; exact action and reason visible |
| PC05 | 6–8 s | Execution | Test command and progress; no network or dependency installation |
| PC06 | 5–7 s | Result | Passing test and concise diff/result state |

Record long handles before and after every action. The final edit will use only 12–19 seconds total.

## Camera and UI treatment

- Capture natively at 4K/60 when possible.
- Desktop UI zoom: 110–125% so Korean text remains readable on a phone.
- Use the real dark theme that best matches the film; do not recolor the product in post.
- Hide cursor except when it communicates approval or navigation.
- No rapid scrolling. One intentional motion per shot.
- Do not overlay fake terminal output or fake PCCP badges.

## Acceptance

- The request, plan, approval, and result form a truthful sequence.
- Only synthetic repository content appears.
- UI is authentic to the recorded revision.
- No real credential or provider endpoint is visible.
- The capture does not imply an existing customer or production deployment.
