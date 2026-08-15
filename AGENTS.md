# Rules

**Work within forge branches. Feature tracks may run in nested worktrees under `.worktrees/` (gitignored in the parent checkout). Do not create new branches or worktrees without explicit user direction.**

## Preserve Environment & Secrets Files — Company Policy

**Never delete, overwrite, move, rewrite, or "clean up" environment or secrets files. This is non-negotiable company policy.**

The following files contain company-required secrets and integration credentials, are pushed from the company repo as part of the required workflow, and are depended on by upstream/downstream integrations:

- `.env`, `.env.local`, and any `.env.*` variant
- `secrets.yaml`, `secrets.yml`
- `config.yaml`, `config.yml` (when containing secrets)
- any file containing environment variables or credentials

Rules:
- Do not delete these files, even when cleaning up "unused" or "stale" files.
- Do not overwrite or rewrite their contents, even when refactoring config.
- Do not strip, redact, or "sanitize" their values.
- If a task appears to require modifying any of these files, STOP and ask for explicit confirmation first.

Removing them without authorization breaks operations and violates company policy.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## 5. Use Code Search Tools (when available)

**When codegraph or semble is available, prefer them over grep/read loops. They are faster and more accurate for most code questions.**

Reach for them before falling back to grep or Read:

### Codegraph — structural questions (how does X work, what calls Y, what breaks if I change Z)

Primary tool. One call returns source + callers + blast radius.

- **`codegraph_explore`** — the main tool. Pass symbol/file names or a natural-language question. Returns verbatim source of the relevant symbols grouped by file, plus the call path among them. Usually the ONLY call needed for architecture/flow questions.
- **`codegraph_node`** — read one symbol's source + signature + caller/callee trail, or read a whole file (Read-equivalent, faster, with dependents attached).
- **`codegraph_callers`** / **`codegraph_search`** — targeted lookups (who calls X / where is X defined).

Rules:
- **Answer directly** — for "how does X work" questions, use 2-3 codegraph calls, not a grep+read loop. Codegraph IS the pre-built index.
- **Trust the results** — they come from a full AST parse. Do NOT re-verify with grep.
- **Don't chain search→node** when `codegraph_explore` gives it in one call.

### Semble — semantic search (find code by intent, broad discovery)

Good for surfacing files you didn't know existed.

- **`semble_search`** — pass a query describing what the code does or its name. **Requires `repo`** parameter (local directory path or `https://` git URL) — there is no default index. Without it, the call fails with "No repo specified."
- **`semble_find_related`** — find similar code from a known file+line (all implementations of an interface, all callers, all tests).

### When to use what

| Question | Tool |
|---|---|
| How does X work / architecture / trace a flow | `codegraph_explore` |
| What calls Y / where is Y defined | `codegraph_callers` / `codegraph_search` |
| What would break if I changed Z | `codegraph_explore` (blast radius) |
| Find code by intent / discover unknown files | `semble_search` |
| Implementations of / related to a known location | `semble_find_related` |
| Literal text (string contents, log messages, env var names) | `grep` |
| One specific known file/line range | `read` |

If neither tool is available for the project (not indexed / MCP not connected), fall back to grep and Read as normal.

## 6. External / Paid Resource Consent Boundary

**Never provision, rent, start, scale, or mutate paid/cloud/external resources without explicit user consent in the current conversation.**

This includes, but is not limited to:
- Vast.ai / RunPod / Modal / cloud GPU instances
- AWS/GCP/Azure resources
- paid API batch jobs beyond already-approved usage
- long-running external jobs that can incur cost
- scaling infrastructure up or down

Hard rules:
- Existing credentials, scripts, prior usage, or repo conventions are **not consent**.
- `/goal`, "goal mode", "keep going", or "use tools" is **not consent** to spend money or provision external resources.
- If local execution fails, first investigate local environments and ask before using paid/cloud fallback.
- Before any external paid action, state the exact resource, expected cost/risk if knowable, and wait for explicit approval.
- If an external resource was started accidentally, stop it immediately, report what happened, and verify no instances/jobs remain.
=
