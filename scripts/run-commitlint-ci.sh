#!/usr/bin/env bash
set -euo pipefail

EVENT_NAME="${CG_EVENT_NAME:-}"
PR_MODE="${CG_PR_MODE:-smart}"
PR_TITLE="${CG_PR_TITLE:-}"
RANGE_FROM="${CG_RANGE_FROM:-}"
RANGE_TO="${CG_RANGE_TO:-}"
IGNORE_BOT_COMMITS="${CG_IGNORE_BOT_COMMITS:-true}"
IGNORE_MERGE_COMMITS="${CG_IGNORE_MERGE_COMMITS:-true}"
IGNORE_MESSAGE_PATTERNS="${CG_IGNORE_MESSAGE_PATTERNS:-}"
COMMITLINT_CMD="${CG_COMMITLINT_CMD:-commitlint}"

is_truthy() {
  case "${1,,}" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

is_pr_event() {
  [[ "$EVENT_NAME" == "pull_request" || "$EVENT_NAME" == "pull_request_target" ]]
}

is_zero_sha() {
  [[ -z "$1" || "$1" =~ ^0+$ ]]
}

is_bot_identity() {
  local value="${1,,}"
  [[ "$value" == *"[bot]"* || "$value" == *"copilot"* || "$value" == *"github-actions"* ]]
}

is_merge_commit() {
  local sha="$1"
  local parent_count

  parent_count="$(git rev-list --parents -n 1 "$sha" | awk '{print NF - 1}')"
  [[ "$parent_count" -gt 1 ]]
}

matches_ignore_pattern() {
  local subject="$1"
  local pattern

  while IFS= read -r pattern; do
    if [[ -z "$pattern" ]]; then
      continue
    fi

    if [[ "$subject" =~ $pattern ]]; then
      return 0
    fi
  done <<< "$IGNORE_MESSAGE_PATTERNS"

  return 1
}

lint_message() {
  local label="$1"
  local message="$2"
  local subject

  subject="$(printf '%s\n' "$message" | head -n 1)"
  echo "linting ${label}: ${subject}"
  printf '%s\n' "$message" | "$COMMITLINT_CMD" --verbose
}

collect_commit_shas() {
  if is_zero_sha "$RANGE_TO"; then
    return 0
  fi

  if is_zero_sha "$RANGE_FROM"; then
    printf '%s\n' "$RANGE_TO"
    return 0
  fi

  git rev-list --reverse "${RANGE_FROM}..${RANGE_TO}"
}

lint_pr_title_if_present() {
  if [[ -z "$PR_TITLE" ]]; then
    echo "error: pr-mode requires a pull request title but none was provided." >&2
    return 1
  fi

  lint_message "PR title" "$PR_TITLE"
}

main() {
  local linted_count=0
  local sha

  if is_pr_event && [[ "$PR_MODE" == "title" ]]; then
    lint_pr_title_if_present
    return 0
  fi

  while IFS= read -r sha; do
    local author_name
    local author_email
    local committer_name
    local committer_email
    local message
    local subject

    if [[ -z "$sha" ]]; then
      continue
    fi

    if is_truthy "$IGNORE_MERGE_COMMITS" && is_merge_commit "$sha"; then
      echo "skipping merge commit ${sha}"
      continue
    fi

    author_name="$(git log -1 --format=%an "$sha")"
    author_email="$(git log -1 --format=%ae "$sha")"
    committer_name="$(git log -1 --format=%cn "$sha")"
    committer_email="$(git log -1 --format=%ce "$sha")"

    if is_truthy "$IGNORE_BOT_COMMITS" && {
      is_bot_identity "$author_name" ||
      is_bot_identity "$author_email" ||
      is_bot_identity "$committer_name" ||
      is_bot_identity "$committer_email"
    }; then
      echo "skipping bot-authored commit ${sha}"
      continue
    fi

    message="$(git log -1 --format=%B "$sha")"
    subject="$(printf '%s\n' "$message" | head -n 1)"

    if matches_ignore_pattern "$subject"; then
      echo "skipping commit ${sha} due to ignore pattern: ${subject}"
      continue
    fi

    lint_message "commit ${sha}" "$message"
    linted_count=$((linted_count + 1))
  done < <(collect_commit_shas)

  if is_pr_event && [[ "$PR_MODE" == "smart" ]] && [[ "$linted_count" -eq 0 ]]; then
    echo "no lintable commits left after filters, falling back to PR title"
    lint_pr_title_if_present
    return 0
  fi

  if [[ "$linted_count" -eq 0 ]]; then
    echo "no commit messages to lint after filters"
  fi
}

main "$@"
