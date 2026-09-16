# Contributing

Keep changes small enough to review and test them against the native checker. The project supports Linux, macOS, and Windows on amd64 and arm64, so avoid shell or path assumptions when a portable implementation is practical.

## Run the checks

From the repository root, run the Go checks and the shell regression suite:

```bash
go vet ./...
go test ./...
bash test/test.sh
```

Build the current checker, then run the action tests against that binary. The integration test is skipped only when `CG_TEST_BINARY` is absent:

```bash
node scripts/build.mjs --current
CG_TEST_BINARY="$(printf '%s\n' dist/commit-guard_* | head -n 1)" node --test action/*.test.js
```

On PowerShell:

```powershell
node scripts/build.mjs --current
$env:CG_TEST_BINARY = (Get-ChildItem .\dist\commit-guard_* | Select-Object -First 1).FullName
node --test action\*.test.js
```

Action tests must use real mocked API fixtures for pagination, fork pull requests, missing ranges, deletion events, configuration decoding, release checksums, annotations, and timeouts. Keep the compiled-binary parity test meaningful: it covers valid headers, case and punctuation, long headers, malformed policy, empty batches, and missing batch input. Do not replace those cases with stubs that merely mirror the implementation.

The release build produces six native targets: Linux, macOS, and Windows on amd64 and arm64. Run the host-specific action tests on each supported runner when changing platform selection, download, permissions, or path handling.

The first Windows prototype measured a single checker start at a median of 50.882 ms for Go and 114.869 ms for Node. That was a minimal prototype rather than a project workload; it is not a completed hook performance benchmark. Measure cold hooks and pre-push batches on all target hosts before making performance claims.

## Change policy and workflow

Use a branch with a descriptive name and make Conventional Commits. Add a focused regression test for behavior that can break. Keep the native checker and action protocol aligned: the action sends a JSON array of `{ "sha", "message" }` objects to `check --batch --config PATH --json`, and expects a JSON report with `valid` and per-commit `errors`.

Document release-worthy behavior in `CHANGELOG.md`. Update `VERSION` only for a release change. Do not commit generated binaries, temporary benchmark output, or credentials.

## Installer changes

Installer changes must preserve unrelated workflow jobs and existing hook bodies. Test installation, migration, stale-version reporting, synchronization, and uninstall in a temporary repository. Verify that release downloads use the checksum manifest and that commit or pre-push hooks do not perform network access.

## Pull requests

Describe the user-visible behavior, the policy or compatibility impact, and the checks you ran. Include the target runner when a change is platform-specific. Keep external workflow references pinned to an exact release or full commit SHA.
