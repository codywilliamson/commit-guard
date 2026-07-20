# commit-guard configuration features — design

Date: 2026-07-19
Status: approved (brainstormed interactively, implementation authorized autonomously)

> **Amendment (same day):** the config format was changed from JSON to flat
> YAML (`.commit-guard.yml`) after review. Rationale: flat YAML is trivially
> and robustly parseable in pure bash (no jq dependency, no formatting
> constraints on the fallback parser), and supports comments in the starter
> config. CI validates syntax with `yq` (preinstalled on runners) and uses it
> to inject `types` into the generated commitlint config; the lint scripts use
> the same bash parser as the hook. Malformed YAML is caught loudly in CI;
> locally the parser is lenient (mis-indented keys fall back to defaults) but
> enum validation still rejects bad values. JSON references below are
> historical.

## Goal

Add a per-repo config file that both CI and local hooks read, plus four new
capabilities: AI-attribution policy, custom allowed types, custom ban patterns,
warn-only enforcement, and branch filters.

## Config file: `.commit-guard.json`

Single source of truth at the repo root. All keys optional. File values win
over workflow inputs; workflow inputs remain as fallbacks when the file or key
is absent. Built-in defaults preserve v0.2 behavior exactly.

```json
{
  "config": "conventional",
  "pr-mode": "smart",
  "enforce": "block",
  "ai-attribution": "block",
  "types": ["feat", "fix", "chore", "ci", "docs", "test", "refactor", "perf", "build", "style"],
  "ban-patterns": ["password", "^temp"],
  "branches": ["main", "master"],
  "ignore-bot-commits": true,
  "ignore-merge-commits": true,
  "ignore-message-patterns": ["^Initial plan$"]
}
```

Keys are kebab-case to mirror workflow inputs. Format is JSON per user choice.

### Parsing strategy

- **CI**: `jq` (preinstalled on GitHub ubuntu runners).
- **Native hook**: `jq` when available, else an inlined sed/awk fallback that
  handles the flat schema. Fallback constraint (documented): arrays must be
  pretty-printed one element per line, and elements must not contain `"`.
  With jq installed there are no constraints.

## Features

### 1. AI attribution policy — `ai-attribution: allow | warn | strip | block`

Built-in case-insensitive patterns matched against the full commit message:
co-authored-by trailers naming AI tools (claude, copilot, chatgpt, openai,
anthropic, gemini, cursor, devin, aider, codex, `[bot]`), "generated with/by"
AI bylines, and `noreply@anthropic.com`.

- `allow` (default): no check — non-breaking for v0.2 upgrades.
- `warn`: print a warning, pass.
- `strip`: local hook rewrites the commit message file, removing matching
  lines, then passes. **In CI, `strip` behaves as `block`** — CI cannot
  rewrite pushed commits, so it acts as the backstop for commits made without
  hooks installed.
- `block`: fail the lint.

The installer writes `"ai-attribution": "block"` into the starter config so
new installs are protected by default while upgrades keep old behavior.

### 2. Custom allowed types — `types`

- Native hook: builds its validation regex from the list.
- CI: generates a commitlint config extending the preset with a `type-enum`
  rule override.
- If the repo has its own commitlint config, that config wins and `types` is
  ignored with a printed notice (avoids two sources of truth in the
  commitlint ecosystem).

### 3. Custom ban patterns — `ban-patterns`

Case-insensitive ERE patterns; if any matches anywhere in the commit message
(or PR title in title lint path), the lint fails. Enforced by the bash layer
in both the hook and CI (commitlint cannot do arbitrary body-regex bans
without a plugin — rejected approach B, publishing a plugin, as YAGNI).

### 4. Warn-only enforcement — `enforce: block | warn`

CI collects all failures instead of exiting on the first, then:
- `block` (default): exit 1 if anything failed.
- `warn`: emit `::warning` annotations and exit 0. For adopting commit-guard
  on messy repos.

Local hook honors it too: `warn` prints the error but allows the commit.

### 5. Branch filters — `branches`

On push events, if the pushed branch is not in the list, CI skips linting
entirely with a notice. Empty/absent list = all branches (current behavior).
Requires passing `github.ref_name` into the lint script.

## Components changed

| File | Change |
|---|---|
| `scripts/validate-commit-message.sh` | read config (jq or inline fallback), custom types regex, ban patterns, ai-attribution incl. strip, enforce warn |
| `scripts/run-commitlint-ci.sh` | jq config read with env fallback, ban patterns, ai-attribution (strip→block), warn mode failure collection, branch filter |
| `.github/workflows/commitlint.yml` | new inputs `enforce`, `ai-attribution`; pass `ref_name`; generate type-enum config from file |
| `install.sh` / `install.ps1` | `--ai-attribution`, `--enforce` flags; write starter `.commit-guard.json` when absent |
| `caller-template.yml` | comment pointing at `.commit-guard.json` |
| `test/*` | native hook: types/ban/ai policies incl. strip; ci: file precedence, ban, warn exit 0, branch skip |
| `README.md`, `CHANGELOG.md` | document schema, precedence, upgrade notes |

The hook stays a single self-contained downloadable file, so the config
parser is inlined there and duplicated (compact) in the CI script rather than
shared via a lib — deployment simplicity beats DRY for shipped artifacts.
Each copy carries a sync note.

## Error handling

- Malformed JSON: jq parse failure → fail loudly with a clear message (never
  silently skip enforcement).
- Unknown enum values (`ai-attribution: "nope"`): fail with the allowed set.
- Missing file: use env/defaults, no error.

## Testing

Extend the existing bash test harness (`test/test.sh`): each feature gets
accept + reject cases; strip mode verifies the message file was rewritten;
warn mode verifies exit 0 with failing content; precedence test verifies file
beats env.
