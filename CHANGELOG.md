# Changelog

## [0.2.2]

- Fix reusable-workflow self-checkout to use `github.job_workflow_sha`. The previous `github.workflow_sha` resolves to the caller's commit in a reusable-workflow context, which caused every run to fail with `remote error: upload-pack: not our ref` when trying to fetch the caller's commit from commit-guard's repo.

## [0.2.1]

- Add retry/backoff around the caller-repo checkout so transient `not our ref` failures on fresh pushes self-heal instead of exhausting `actions/checkout`'s 7-second internal retry budget.

## [0.2.0]

- Add smart PR commitlint handling with PR title fallback for noisy bot and plan commits.
- Add configurable PR lint strategy and broader local hook support with tracked native git hooks.
- Add lightweight version metadata, release tooling, and shell-based regression tests.

## [0.1.0]

- Initial public release with reusable commitlint workflow and installer scripts.
