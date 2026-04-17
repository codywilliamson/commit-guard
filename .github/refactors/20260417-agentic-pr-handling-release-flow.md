Goal

Reduce noisy commitlint failures on agentic PRs, add broader local hook enforcement, and give the repo a simple repeatable version/release flow.

Scope

- `.github/workflows/commitlint.yml`
- `caller-template.yml`
- `install.sh`
- `install.ps1`
- `README.md`
- `CONTRIBUTING.md`
- new `scripts/` and `test/` helpers
- version/release metadata files

Proposed steps

- Add lightweight version metadata and a small release script so the repo can cut and bump releases intentionally.
- Move CI commit linting into a helper script with configurable PR behavior and skip rules for noisy bot/plan commits.
- Add a tracked git hook option that works outside Node/Husky repos while keeping Husky support for Node projects.
- Add focused shell tests around commit-range selection, skip behavior, and installer output.
- Update docs and examples to show the new defaults and the knobs for stricter or looser enforcement.

Risks / test notes

- Reusable workflow input changes must remain backward compatible for existing consumers.
- Native git hook setup must avoid clobbering custom hook paths unexpectedly.
- Tests should use temp repos and local fixtures so they do not depend on network access.
