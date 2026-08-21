# Method 02 — Safe Demo Environment

## Non-negotiable isolation

All screen capture uses synthetic data and temporary state. Never record a real repository, real user session, real credential, production hostname, customer name, token, terminal history, notification, or desktop background.

Do not run `scripts/demo.py` directly in the main PCCP checkout: it terminates matching processes and deletes `.data/pccp.db`. Use a disposable source copy and temporary data directory instead.

## Workspace layout

```bash
capture_root="$(mktemp -d /tmp/patty-film-capture.XXXXXX)"
mkdir -p "$capture_root/pccp" "$capture_root/patty-code-home" "$capture_root/synthetic-repo" "$capture_root/recordings"
```

Record the path in the production log. Never use `$HOME`, `~`, or a workspace root as a cleanup target.

## Disposable PCCP copy

Create a source-only copy that excludes Git history, databases, binaries, environment files, keys, and secret/config files:

```bash
rsync -a \
  --exclude='.git' \
  --exclude='.env*' \
  --exclude='.data' \
  --exclude='.keys' \
  --exclude='bin' \
  --exclude='web/node_modules' \
  --exclude='secrets.yaml' \
  --exclude='secrets.yml' \
  --exclude='config.yaml' \
  --exclude='config.yml' \
  /Users/patrickrho/projects/pccp/ "$capture_root/pccp/"
```

The original checkout remains untouched. Before starting, verify that the disposable copy contains no `.env*`, `.keys`, `secrets.yaml`, `secrets.yml`, `config.yaml`, or `config.yml` files.

## Synthetic repository

Create a small local application named `seoul-mobility-demo` containing only invented data. It should have:

- one failing unit test caused by an idempotency bug;
- a safe, reviewable two-file fix;
- Korean comments and test names;
- no package installation, network calls, or destructive commands;
- Git initialized locally with no remote.

The staged Patty Code prompt is:

> 결제 재시도 시 중복 처리되는 테스트 실패 원인을 찾고, 먼저 수정 계획을 보여준 뒤 승인받은 범위에서만 수정하고 테스트해줘.

The recording must show planning, a bounded edit, approval, test execution, and result. It must not pretend the demo repository is a customer project.

## Capture hygiene

- Set desktop notifications and messaging clients to Do Not Disturb.
- Use a fresh OS user profile if available; otherwise hide menu-bar account names and personal icons.
- Use a neutral 4K background.
- Browser profile must be temporary and contain no saved accounts, extensions, or history.
- Check every frame for tokens, email addresses, local usernames, filesystem paths, Wi-Fi SSIDs, and unrelated tabs.
- Use synthetic names already present in fixtures only where clearly fictional.

## Cleanup

After selected recordings have been copied and checksummed, terminate only the PIDs recorded by the capture session. Move the temporary directory to trash or remove the exact validated path. Never use an unresolved variable or broad recursive target.
