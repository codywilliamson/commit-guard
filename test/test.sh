#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
command -v go >/dev/null
go vet ./...
go test ./...
node "$ROOT_DIR/scripts/build.mjs" --current
if [[ "${OS:-}" == Windows_NT || "${RUNNER_OS:-}" == Windows ]]; then
  binary="$(find "$ROOT_DIR/dist" -maxdepth 1 -type f -name 'commit-guard_*_windows_*.exe' | head -1)"
else
  binary="$(find "$ROOT_DIR/dist" -maxdepth 1 -type f -name 'commit-guard_*' ! -name '*.exe' | head -1)"
fi
if [[ -n "$binary" ]]; then
  if command -v cygpath >/dev/null 2>&1; then binary="$(cygpath -w "$binary")"; fi
  CG_TEST_BINARY="$binary" node --test "$ROOT_DIR"/action/*.test.js
fi
