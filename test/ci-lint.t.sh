#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

test_smart_pr_mode_falls_back_to_pr_title_when_only_bot_commits_are_skipped() {
  local repo_dir
  local base_sha
  local head_sha
  local temp_dir
  local output

  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_git_repo "$repo_dir"

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  commit_file \
    "$repo_dir" \
    "plan.txt" \
    "bot plan" \
    "Initial plan" \
    "copilot-swe-agent[bot]" \
    "copilot-swe-agent[bot]@users.noreply.github.com"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="pull_request" \
    CG_PR_MODE="smart" \
    CG_PR_TITLE="feat: improve agentic PR handling" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
    CG_IGNORE_BOT_COMMITS="true" \
    CG_IGNORE_MERGE_COMMITS="true" \
    CG_IGNORE_MESSAGE_PATTERNS="^Initial plan$" \
      "${ROOT_DIR}/scripts/run-commitlint-ci.sh"
  )

  output="$(cat "${temp_dir}/commitlint.log")"
  assert_contains "feat: improve agentic PR handling" "$output" "expected PR title to be linted"
  if [[ "$output" == *"Initial plan"* ]]; then
    fail "expected bot plan commit to be skipped"
  fi
}

test_push_mode_still_fails_bad_human_commits() {
  local repo_dir
  local base_sha
  local head_sha
  local temp_dir

  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_git_repo "$repo_dir"

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  commit_file "$repo_dir" "notes.txt" "bad" "Initial plan"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  if (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="push" \
    CG_PR_MODE="smart" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
    CG_IGNORE_BOT_COMMITS="true" \
    CG_IGNORE_MERGE_COMMITS="true" \
      "${ROOT_DIR}/scripts/run-commitlint-ci.sh"
  ); then
    fail "expected push lint to fail on bad human commit"
  fi
}

test_smart_pr_mode_favors_real_commits_over_title_fallback() {
  local repo_dir
  local base_sha
  local head_sha
  local temp_dir
  local output

  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_git_repo "$repo_dir"

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  commit_file "$repo_dir" "feature.txt" "good" "fix: tighten commit selection"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="pull_request" \
    CG_PR_MODE="smart" \
    CG_PR_TITLE="feat: fallback should not run here" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
    CG_IGNORE_BOT_COMMITS="true" \
    CG_IGNORE_MERGE_COMMITS="true" \
      "${ROOT_DIR}/scripts/run-commitlint-ci.sh"
  )

  output="$(cat "${temp_dir}/commitlint.log")"
  assert_contains "fix: tighten commit selection" "$output" "expected real commit to be linted"
  if [[ "$output" == *"feat: fallback should not run here"* ]]; then
    fail "expected PR title fallback to stay unused when a real commit is linted"
  fi
}

test_smart_pr_mode_falls_back_to_pr_title_when_patterns_skip_all_commits() {
  local repo_dir
  local base_sha
  local head_sha
  local temp_dir
  local output

  temp_dir="$(make_temp_dir)"
  repo_dir="${temp_dir}/repo"
  make_git_repo "$repo_dir"

  commit_file "$repo_dir" "README.md" "base" "feat: seed repo"
  base_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  commit_file "$repo_dir" "plan.txt" "draft" "Initial plan"
  head_sha="$(git -C "$repo_dir" rev-parse HEAD)"

  make_commitlint_stub "${temp_dir}/bin" "${temp_dir}/commitlint.log"

  (
    cd "$repo_dir"
    PATH="${temp_dir}/bin:${PATH}" \
    COMMITLINT_LOG="${temp_dir}/commitlint.log" \
    CG_EVENT_NAME="pull_request" \
    CG_PR_MODE="smart" \
    CG_PR_TITLE="fix: use PR title fallback for ignored commits" \
    CG_RANGE_FROM="$base_sha" \
    CG_RANGE_TO="$head_sha" \
    CG_IGNORE_BOT_COMMITS="false" \
    CG_IGNORE_MERGE_COMMITS="true" \
    CG_IGNORE_MESSAGE_PATTERNS="^Initial plan$" \
      "${ROOT_DIR}/scripts/run-commitlint-ci.sh"
  )

  output="$(cat "${temp_dir}/commitlint.log")"
  assert_contains "fix: use PR title fallback for ignored commits" "$output" "expected PR title fallback after pattern skip"
  if [[ "$output" == *"Initial plan"* ]]; then
    fail "expected ignored plan commit to stay out of lint input"
  fi
}

test_smart_pr_mode_falls_back_to_pr_title_when_only_bot_commits_are_skipped
test_push_mode_still_fails_bad_human_commits
test_smart_pr_mode_favors_real_commits_over_title_fallback
test_smart_pr_mode_falls_back_to_pr_title_when_patterns_skip_all_commits
