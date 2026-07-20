#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

VALIDATOR="${ROOT_DIR}/scripts/validate-commit-message.sh"
CI_SCRIPT="${ROOT_DIR}/scripts/run-commitlint-ci.sh"

make_config_repo() {
  local repo_dir="$1"
  local config_json="$2"

  make_git_repo "$repo_dir"
  printf '%s\n' "$config_json" > "${repo_dir}/.commit-guard.json"
}

run_validator() {
  local repo_dir="$1"
  local message="$2"
  local message_file="${repo_dir}/message.txt"

  printf '%s\n' "$message" > "$message_file"
  (cd "$repo_dir" && "$VALIDATOR" message.txt)
}

test_custom_types_accept_and_reject() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "types": ["feat", "wip"]
}'

  run_validator "$repo_dir" "wip: half-done thing"

  if run_validator "$repo_dir" "chore: not in the custom list" 2>/dev/null; then
    fail "expected type outside custom list to be rejected"
  fi
}

test_custom_types_with_fallback_parser() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "types": [
    "feat",
    "wip"
  ]
}'

  (
    export CG_FORCE_FALLBACK_PARSER=1
    run_validator "$repo_dir" "wip: fallback parser works"
    if run_validator "$repo_dir" "chore: rejected via fallback" 2>/dev/null; then
      fail "expected fallback parser to enforce custom types"
    fi
  )
}

test_ban_patterns_reject_matching_message() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "ban-patterns": ["password", "^temp"]
}'

  if run_validator "$repo_dir" "feat: add Password rotation" 2>/dev/null; then
    fail "expected banned pattern to reject message"
  fi

  run_validator "$repo_dir" "feat: add pin rotation"
}

test_ai_attribution_block() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "ai-attribution": "block"
}'

  if run_validator "$repo_dir" "feat: add thing

Co-Authored-By: Claude <noreply@anthropic.com>" 2>/dev/null; then
    fail "expected AI co-author trailer to be blocked"
  fi

  if run_validator "$repo_dir" "feat: add thing

🤖 Generated with Claude Code" 2>/dev/null; then
    fail "expected AI byline to be blocked"
  fi

  run_validator "$repo_dir" "feat: add thing

Co-Authored-By: Human Person <human@example.com>"
}

test_ai_attribution_warn_allows() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "ai-attribution": "warn"
}'

  run_validator "$repo_dir" "feat: add thing

Co-Authored-By: Claude <noreply@anthropic.com>"
}

test_ai_attribution_strip_rewrites_message() {
  local repo_dir
  local result
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "ai-attribution": "strip"
}'

  run_validator "$repo_dir" "feat: add thing

Co-Authored-By: Claude <noreply@anthropic.com>"

  result="$(cat "${repo_dir}/message.txt")"
  if [[ "$result" == *"Claude"* ]]; then
    fail "expected AI trailer to be stripped from message file"
  fi
  assert_contains "feat: add thing" "$result" "expected real content to survive strip"
}

test_enforce_warn_allows_bad_commit_locally() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "enforce": "warn"
}'

  run_validator "$repo_dir" "totally not conventional"
}

test_invalid_enum_fails_loudly() {
  local repo_dir
  repo_dir="$(make_temp_dir)/repo"
  make_config_repo "$repo_dir" '{
  "ai-attribution": "nope"
}'

  if run_validator "$repo_dir" "feat: fine subject" 2>/dev/null; then
    fail "expected invalid ai-attribution value to fail"
  fi
}

test_ci_file_overrides_env_enforce() {
  local temp_dir repo_dir base_sha head_sha
  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_config_repo "$repo_dir" '{
  "enforce": "warn"
}'

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"
  commit_file "$repo_dir" "notes.txt" "bad" "Initial plan"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="push" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
    CG_ENFORCE="block" \
      "$CI_SCRIPT"
  ) || fail "expected enforce=warn from config file to pass despite bad commit"
}

test_ci_ban_pattern_fails_commit() {
  local temp_dir repo_dir base_sha head_sha
  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_config_repo "$repo_dir" '{
  "ban-patterns": ["hunter2"]
}'

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"
  commit_file "$repo_dir" "notes.txt" "x" "feat: set password to hunter2"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  if (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="push" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
      "$CI_SCRIPT"
  ); then
    fail "expected banned pattern to fail CI lint"
  fi
}

test_ci_ai_attribution_strip_acts_as_block() {
  local temp_dir repo_dir base_sha head_sha
  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_config_repo "$repo_dir" '{
  "ai-attribution": "strip"
}'

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"
  commit_file "$repo_dir" "notes.txt" "x" "feat: add thing

Co-Authored-By: Claude <noreply@anthropic.com>"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  if (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="push" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
      "$CI_SCRIPT"
  ); then
    fail "expected strip policy to act as block in CI"
  fi
}

test_ci_branch_filter_skips_other_branches() {
  local temp_dir repo_dir base_sha head_sha output
  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_config_repo "$repo_dir" '{
  "branches": ["main", "master"]
}'

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"
  commit_file "$repo_dir" "notes.txt" "bad" "Initial plan"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  output="$(
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="push" \
    CG_REF_NAME="feature/foo" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
      "$CI_SCRIPT"
  )" || fail "expected branch filter to skip lint and pass"

  assert_contains "skipping lint" "$output" "expected skip notice for filtered branch"
}

test_custom_types_accept_and_reject
test_custom_types_with_fallback_parser
test_ban_patterns_reject_matching_message
test_ai_attribution_block
test_ai_attribution_warn_allows
test_ai_attribution_strip_rewrites_message
test_enforce_warn_allows_bad_commit_locally
test_invalid_enum_fails_loudly
test_ci_file_overrides_env_enforce
test_ci_ban_pattern_fails_commit
test_ci_ai_attribution_strip_acts_as_block
test_ci_branch_filter_skips_other_branches
