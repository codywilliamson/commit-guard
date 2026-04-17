#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

test_native_validator_accepts_conventional_commit() {
  local temp_dir
  local message_file

  temp_dir="$(make_temp_dir)"
  message_file="${temp_dir}/message.txt"
  printf 'feat(cli): add native hook mode\n' > "$message_file"

  "${ROOT_DIR}/scripts/validate-commit-message.sh" "$message_file"
}

test_native_validator_rejects_non_conventional_commit() {
  local temp_dir
  local message_file

  temp_dir="$(make_temp_dir)"
  message_file="${temp_dir}/message.txt"
  printf 'Initial plan\n' > "$message_file"

  if "${ROOT_DIR}/scripts/validate-commit-message.sh" "$message_file"; then
    fail "expected native validator to reject invalid commit message"
  fi
}

test_native_validator_allows_merge_and_fixup_messages() {
  local temp_dir
  local merge_file
  local fixup_file

  temp_dir="$(make_temp_dir)"
  merge_file="${temp_dir}/merge.txt"
  fixup_file="${temp_dir}/fixup.txt"

  printf 'Merge branch '\''main'\'' into feature\n' > "$merge_file"
  printf 'fixup! feat: add hook mode\n' > "$fixup_file"

  "${ROOT_DIR}/scripts/validate-commit-message.sh" "$merge_file"
  "${ROOT_DIR}/scripts/validate-commit-message.sh" "$fixup_file"
}

test_native_validator_accepts_conventional_commit
test_native_validator_rejects_non_conventional_commit
test_native_validator_allows_merge_and_fixup_messages
