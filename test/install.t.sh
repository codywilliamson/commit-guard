#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

test_installer_supports_native_hooks_and_pr_title_mode_without_node_repo() {
  local repo_dir
  local output
  local temp_dir

  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_git_repo "$repo_dir"

  (
    cd "$repo_dir"
    COMMIT_GUARD_TEMPLATE_URL="file://${ROOT_DIR}/caller-template.yml" \
    COMMIT_GUARD_VALIDATOR_URL="file://${ROOT_DIR}/scripts/validate-commit-message.sh" \
      "${ROOT_DIR}/install.sh" --hook-mode native --pr-mode title
  ) > "${temp_dir}/install.log"

  output="$(cat "${temp_dir}/install.log")"
  assert_contains "installed: .github/workflows/commitlint.yml" "$output" "expected workflow install output"
  assert_contains "created: .githooks/commit-msg" "$output" "expected native hook install output"
  assert_contains 'pr-mode: "title"' "$(cat "${repo_dir}/.github/workflows/commitlint.yml")" "expected PR mode to be rendered"
  assert_contains ".githooks" "$(git -C "$repo_dir" config --get core.hooksPath)" "expected git hooks path to point at tracked hooks"
}

test_installer_supports_native_hooks_and_pr_title_mode_without_node_repo
