# commit-guard

One-liner setup for [conventional commits](https://www.conventionalcommits.org/) enforcement with a reusable GitHub Actions workflow plus local git hooks.

## Install

From your project root:

```bash
# bash — install CI + local hooks
curl -sL https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.sh | bash

# bash — CI only
curl -sL https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.sh | bash -s -- --ci-only

# powershell
irm https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.ps1 | iex
```

Common options:

```bash
./install.sh --config angular
./install.sh --pr-mode title
./install.sh --hook-mode native
./install.sh --hook-mode husky --pm pnpm
./install.sh --ai-attribution strip
./install.sh --enforce warn
./install.sh --ci-only
```

## Configuration file

Both CI and local hooks read an optional `.commit-guard.json` at the repo root.
File values override workflow inputs; workflow inputs apply when the file or
key is absent. All keys are optional.

```json
{
  "config": "conventional",
  "pr-mode": "smart",
  "enforce": "block",
  "ai-attribution": "block",
  "types": ["feat", "fix", "chore", "ci", "docs", "test", "refactor", "perf", "build", "style"],
  "ban-patterns": ["password"],
  "branches": ["main", "master"],
  "ignore-bot-commits": true,
  "ignore-merge-commits": true,
  "ignore-message-patterns": ["^Initial plan$"]
}
```

- `enforce`: `block` (default) fails on violations; `warn` annotates and passes.
  Useful when adopting commit-guard on an existing repo.
- `ai-attribution`: policy for AI co-author trailers and "generated with"
  bylines (`Co-Authored-By: Claude ...`, `🤖 Generated with Claude Code`).
  - `allow` (default): no check
  - `warn`: report but pass
  - `strip`: the local hook removes matching lines before the commit lands; in
    CI this acts as `block` since pushed commits can't be rewritten
  - `block`: fail the lint
- `types`: custom allowed commit types. In CI this generates a commitlint
  `type-enum` rule; if the repo has its own commitlint config, that config wins
  and `types` is ignored.
- `ban-patterns`: case-insensitive regexes that fail the lint when matched
  anywhere in the commit message (or PR title).
- `branches`: on push events, only these branches are linted. Empty or absent
  means all branches.

The native hook parses the file with `jq` when available. Without `jq`, a
built-in fallback parser is used — keep the file flat and pretty-printed with
one array element per line, and avoid `"` inside values.

The installer writes a starter `.commit-guard.json` with
`"ai-attribution": "block"` for new installs; existing configs are never
overwritten.

## What changed in v0.2.0

- PR linting is now configurable with `smart`, `commits`, and `title` modes.
- `smart` is the new default for PRs. It skips noisy bot commits, skips optional ignored subjects, and falls back to linting the PR title if no real commits remain.
- Local hooks now support a native tracked git hook mode for non-Node repos instead of silently dropping to CI-only.
- The installers now support `--pr-mode` and `--hook-mode`.

## Upgrade from v0.1.0

If you already installed `commit-guard`:

1. Re-run the installer in your repo.
2. Pick a PR strategy:
   - `--pr-mode smart` keeps commit linting but avoids noisy agent/bot plan commits.
   - `--pr-mode title` is best if you squash merge and only care about the final PR title.
   - `--pr-mode commits` keeps the old strict branch-history behavior.
3. If your repo is not a Node repo, use `--hook-mode native` or just let `auto` choose it for you.
4. Commit the updated workflow and hook files.

If you previously used the v0.1.0 defaults, `smart` mode is the main behavior change to be aware of.

## What gets installed

**CI workflow**:
- `.github/workflows/commitlint.yml` validates commit messages on push and PRs

**Local hooks**:
- `husky` mode for Node repos
- native tracked git hooks for non-Node repos

**Optional config**:
- `commitlint.config.js` when Husky mode is installed into a Node repo without an existing commitlint config

## PR lint modes

`smart`:
- On pushes, lint commit messages in the pushed range.
- On PRs, lint commit messages in the PR range.
- Skip bot-authored commits by default.
- Skip merge commits by default.
- Fall back to linting the PR title when every commit in the PR was skipped.

`commits`:
- Always lint the commit range.

`title`:
- On PRs, lint only the PR title.
- On pushes, lint the pushed commit range.

## Hook modes

`auto`:
- Uses Husky when a package manager is detected.
- Uses native tracked git hooks otherwise.

`husky`:
- Installs `@commitlint/cli`, the selected preset, and `husky`.
- Creates `.husky/commit-msg`.

`native`:
- Downloads a lightweight `commit-msg` hook into your repo's hook path.
- Uses `core.hooksPath` with `.githooks` by default.
- Validates standard Conventional Commit headers without requiring Node dependencies.

`none`:
- Installs only the CI workflow.

## Example caller workflow

The reusable workflow is consumed like this:

```yaml
name: Commitlint

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

permissions:
  contents: read

jobs:
  commitlint:
    uses: codywilliamson/commit-guard/.github/workflows/commitlint.yml@v0.2.2
    with:
      config: "conventional"
      pr-mode: "smart"
      # enforce: "warn"
      # ai-attribution: "block"
      # ignore-message-patterns: |
      #   ^Initial plan$
```

## Conventional commit format

```text
type(scope): description
```

Common types:

- `feat`
- `fix`
- `chore`
- `ci`
- `test`
- `refactor`
- `docs`
- `style`
- `perf`
- `build`

## Release flow

This repo now tracks its current release in `VERSION` and notes changes in `CHANGELOG.md`.

To cut a release:

```bash
./scripts/release.sh
```

## Requirements

- GitHub Actions enabled
- Node.js 18+ for Husky mode
- Git for local native hooks
- This repo must be public for cross-repo reusable workflow access

## License

MIT
