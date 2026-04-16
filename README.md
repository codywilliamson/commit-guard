# commit-guard

One-liner setup for [conventional commits](https://www.conventionalcommits.org/) enforcement — local git hooks + CI validation via a reusable GitHub Actions workflow.

## Install

From your project root:

```bash
# bash — full install (local hooks + CI)
curl -sL https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.sh | bash

# bash — CI only (no local hooks, good for non-Node repos)
curl -sL https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.sh | bash -s -- --ci-only

# powershell
irm https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.ps1 | iex
```

Options:

```bash
./install.sh --config conventional  # or angular
./install.sh --pm pnpm              # force package manager (auto-detected)
./install.sh --ci-only              # skip local hooks
```

## What gets installed

**CI workflow** (always):
- `.github/workflows/commitlint.yml` — validates commit messages on push and PRs

**Local hooks** (Node.js repos):
- `commitlint.config.js` — extends `@commitlint/config-conventional`
- `.husky/commit-msg` — runs commitlint on every commit
- devDependencies: `@commitlint/cli`, `@commitlint/config-conventional`, `husky`

## How it works

```
Local (pre-push safety net)          CI (enforced gate)
┌─────────────────────┐    ┌──────────────────────────────┐
│ git commit -m "..."  │    │ push / PR                     │
│   └─ husky hook      │    │   └─ reusable workflow         │
│       └─ commitlint  │    │       └─ commitlint --from/to  │
│           ✓ or ✗     │    │           ✓ or ✗               │
└─────────────────────┘    └──────────────────────────────┘
```

Local hooks catch bad commits before they leave your machine. CI catches anything that slips through (force-push, hooks disabled, external contributors).

## Conventional commit format

```
type(scope): description

types: feat, fix, chore, ci, test, refactor, docs, style, perf, build
scope: optional, e.g. feat(api): add auth endpoint
```

## Configuration

Edit `commitlint.config.js` to add custom rules:

```js
export default {
  extends: ["@commitlint/config-conventional"],
  rules: {
    "scope-enum": [2, "always", ["api", "ui", "db", "ci"]],
    "subject-case": [2, "always", "lower-case"],
  },
};
```

## Requirements

- GitHub Actions enabled
- Node.js 18+ (for local hooks; CI handles its own runtime)
- This repo must be **public** (for cross-repo reusable workflow access)

## License

MIT
