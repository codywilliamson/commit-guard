# Changelog

## [0.3.0] - 2026-09-16

Version 0.3.0 makes a native checker the shared implementation for local hooks and CI. It keeps the selected workflow release and local installation aligned without installing packages during validation.

### Added

- A dependency-free Node 24 GitHub Action at `codywilliamson/commit-guard@v0.3.0`.
- API-based commit collection for pull requests, pushes, merge queues, and explicit workflow dispatch ranges, with complete pagination checks and fork support.
- Pull request title mode, while push events always validate commit ranges.
- A versioned native checker for Linux, macOS, and Windows on amd64 and arm64.
- Strict `.commit-guard.json` policy loading with optional types, header length, merge-commit, and fixup-commit settings.
- SHA-256 verification for release assets and Dependabot configuration for the pinned GitHub Action reference.
- Repository-local `git commit-guard sync`, `status`, and `uninstall` commands.

### Changed

- The default header length is unlimited. Set `maxHeaderLength` when a repository wants a limit.
- Local commit and pre-push checks use one installed native checker and one batch invocation for outgoing commits.
- Clean installs use native executables in Git's private hooks directory. Existing custom shell hooks keep a managed block and backups; exact old generated validators are replaced.
- The legacy reusable workflow remains available at `.github/workflows/commitlint.yml@v0.3.0`; its accepted configuration is limited to the standard policy and `pr-mode`.
- `smart` remains accepted as a compatibility alias for `commits`. It no longer skips bot commits or falls back to a title.

### Breaking changes

- JavaScript commitlint configuration and npm runtime dependencies are no longer supported. Move policy to `.commit-guard.json` using the fields documented in [configuration.md](docs/configuration.md).
- Each clone needs local installation. Hook executables live in Git's private directory; the installer adds a repository-local Git alias without changing global PATH.
- Unsupported legacy reusable-workflow options now fail clearly instead of changing the native policy.

### Migration

1. Re-run the v0.3.0 installer from the repository root. It keeps a backup when it migrates an older generated workflow and preserves unrelated jobs.
2. Replace JavaScript commitlint settings with `.commit-guard.json`. A missing file uses the default policy, including no header limit.
3. Review the generated workflow's pinned `codywilliamson/commit-guard@v0.3.0` reference. Dependabot uses that reference as the update source.
4. Run `git commit-guard status`, then `git commit-guard sync` if the local copy is stale.

## [0.2.2]

- Fix reusable-workflow self-checkout to use `github.job_workflow_sha`. The previous `github.workflow_sha` resolves to the caller's commit in a reusable-workflow context, which caused every run to fail with `remote error: upload-pack: not our ref` when trying to fetch the caller's commit from commit-guard's repo.

## [0.2.1]

- Add retry/backoff around the caller-repo checkout so transient `not our ref` failures on fresh pushes self-heal instead of exhausting `actions/checkout`'s 7-second internal retry budget.

## [0.2.0]

- Add configurable PR lint strategy and broader local hook support with tracked native git hooks.
- Add lightweight version metadata, release tooling, and shell-based regression tests.

## [0.1.0]

- Initial public release with reusable commitlint workflow and installer.
