# Changelog

## [0.2.1]

- Fix transient "not our ref" failures on fresh pushes by replacing the caller checkout with a retry-aware clone that backs off up to six times before giving up.

## [0.2.0]

- Add smart PR commitlint handling with PR title fallback for noisy bot and plan commits.
- Add configurable PR lint strategy and broader local hook support with tracked native git hooks.
- Add lightweight version metadata, release tooling, and shell-based regression tests.

## [0.1.0]

- Initial public release with reusable commitlint workflow and installer scripts.
