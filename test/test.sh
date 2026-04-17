#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for test_file in "${ROOT_DIR}"/test/*.t.sh; do
  echo "Running $(basename "$test_file")"
  bash "$test_file"
done
