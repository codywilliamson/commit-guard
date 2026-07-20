#!/usr/bin/env bash
set -euo pipefail

# reads optional .commit-guard.json in the caller repo root; file values win
# over CG_* env fallbacks. config parsing mirrors validate-commit-message.sh —
# keep in sync. jq is preinstalled on github runners.

EVENT_NAME="${CG_EVENT_NAME:-}"
PR_MODE="${CG_PR_MODE:-smart}"
PR_TITLE="${CG_PR_TITLE:-}"
RANGE_FROM="${CG_RANGE_FROM:-}"
RANGE_TO="${CG_RANGE_TO:-}"
REF_NAME="${CG_REF_NAME:-}"
IGNORE_BOT_COMMITS="${CG_IGNORE_BOT_COMMITS:-true}"
IGNORE_MERGE_COMMITS="${CG_IGNORE_MERGE_COMMITS:-true}"
IGNORE_MESSAGE_PATTERNS="${CG_IGNORE_MESSAGE_PATTERNS:-}"
ENFORCE="${CG_ENFORCE:-block}"
AI_ATTRIBUTION="${CG_AI_ATTRIBUTION:-allow}"
BAN_PATTERNS=""
BRANCHES=""
COMMITLINT_CMD="${CG_COMMITLINT_CMD:-commitlint}"

CONFIG_FILE=".commit-guard.json"

AI_ATTRIBUTION_PATTERNS=(
  '^co-authored-by:.*(claude|copilot|chatgpt|openai|anthropic|gemini|cursor|devin|aider|codex|\[bot\])'
  'generated (with|by).*(claude|chatgpt|copilot|gemini|cursor|aider|codex)'
  'noreply@anthropic\.com'
)

FAILURE_COUNT=0

load_config_file() {
  if [[ ! -f "$CONFIG_FILE" ]]; then
    return 0
  fi

  if ! command -v jq >/dev/null 2>&1; then
    echo "warning: ${CONFIG_FILE} found but jq is unavailable, using workflow inputs only." >&2
    return 0
  fi

  if ! jq empty "$CONFIG_FILE" 2>/dev/null; then
    echo "error: ${CONFIG_FILE} is not valid JSON." >&2
    exit 1
  fi

  file_get() {
    local key="$1"
    local fallback="$2"
    local value

    value="$(jq -r --arg k "$key" 'if has($k) then .[$k] | tostring else "" end' "$CONFIG_FILE")"
    printf '%s\n' "${value:-$fallback}"
  }

  file_get_array() {
    jq -r --arg k "$1" '.[$k] // [] | .[]' "$CONFIG_FILE"
  }

  PR_MODE="$(file_get pr-mode "$PR_MODE")"
  ENFORCE="$(file_get enforce "$ENFORCE")"
  AI_ATTRIBUTION="$(file_get ai-attribution "$AI_ATTRIBUTION")"
  IGNORE_BOT_COMMITS="$(file_get ignore-bot-commits "$IGNORE_BOT_COMMITS")"
  IGNORE_MERGE_COMMITS="$(file_get ignore-merge-commits "$IGNORE_MERGE_COMMITS")"

  local file_ignore_patterns
  file_ignore_patterns="$(file_get_array ignore-message-patterns)"
  if [[ -n "$file_ignore_patterns" ]]; then
    IGNORE_MESSAGE_PATTERNS="$file_ignore_patterns"
  fi

  BAN_PATTERNS="$(file_get_array ban-patterns)"
  BRANCHES="$(file_get_array branches)"

  echo "loaded ${CONFIG_FILE} (file values override workflow inputs)"
}

validate_enums() {
  case "$ENFORCE" in
    block|warn) ;;
    *)
      echo "error: invalid enforce value '${ENFORCE}'. expected block or warn." >&2
      exit 1
      ;;
  esac

  case "$AI_ATTRIBUTION" in
    allow|warn|strip|block) ;;
    *)
      echo "error: invalid ai-attribution value '${AI_ATTRIBUTION}'. expected allow, warn, strip, or block." >&2
      exit 1
      ;;
  esac
}

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

branch_filter_skips_push() {
  if is_pr_event || [[ -z "$BRANCHES" ]]; then
    return 1
  fi

  local branch
  while IFS= read -r branch; do
    if [[ "$branch" == "$REF_NAME" ]]; then
      return 1
    fi
  done <<< "$BRANCHES"

  return 0
}

record_failure() {
  FAILURE_COUNT=$((FAILURE_COUNT + 1))
  if [[ "$ENFORCE" == "warn" ]]; then
    echo "::warning::${1}"
  else
    echo "::error::${1}"
  fi
}

find_ai_attribution_lines() {
  local message="$1"
  local line
  local lowered
  local pattern

  while IFS= read -r line; do
    lowered="${line,,}"
    for pattern in "${AI_ATTRIBUTION_PATTERNS[@]}"; do
      if [[ "$lowered" =~ $pattern ]]; then
        printf '%s\n' "$line"
        break
      fi
    done
  done <<< "$message"
}

check_ai_attribution() {
  local label="$1"
  local message="$2"
  local matches

  if [[ "$AI_ATTRIBUTION" == "allow" ]]; then
    return 0
  fi

  matches="$(find_ai_attribution_lines "$message")"
  if [[ -z "$matches" ]]; then
    return 0
  fi

  if [[ "$AI_ATTRIBUTION" == "warn" ]]; then
    echo "::warning::${label} contains AI attribution: ${matches}"
    return 0
  fi

  # strip cannot rewrite pushed commits, so it acts as block in ci
  record_failure "${label} contains AI attribution: ${matches}"
  return 1
}

check_ban_patterns() {
  local label="$1"
  local message="$2"
  local pattern
  local ok=0

  while IFS= read -r pattern; do
    if [[ -z "$pattern" ]]; then
      continue
    fi
    if grep -Eiq -- "$pattern" <<< "$message"; then
      record_failure "${label} matches banned pattern: ${pattern}"
      ok=1
    fi
  done <<< "$BAN_PATTERNS"

  return "$ok"
}

lint_message() {
  local label="$1"
  local message="$2"
  local subject
  local ok=0

  subject="$(printf '%s\n' "$message" | head -n 1)"
  echo "linting ${label}: ${subject}"

  if ! printf '%s\n' "$message" | "$COMMITLINT_CMD" --verbose; then
    record_failure "${label} failed commitlint: ${subject}"
    ok=1
  fi

  if ! check_ai_attribution "$label" "$message"; then
    ok=1
  fi

  if ! check_ban_patterns "$label" "$message"; then
    ok=1
  fi

  return "$ok"
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
    exit 1
  fi

  lint_message "PR title" "$PR_TITLE" || true
}

finish() {
  if [[ "$FAILURE_COUNT" -eq 0 ]]; then
    return 0
  fi

  if [[ "$ENFORCE" == "warn" ]]; then
    echo "::warning::commit-guard found ${FAILURE_COUNT} issue(s); enforce is 'warn', passing anyway."
    return 0
  fi

  echo "commit-guard found ${FAILURE_COUNT} issue(s)." >&2
  return 1
}

main() {
  local linted_count=0
  local sha

  load_config_file
  validate_enums

  if branch_filter_skips_push; then
    echo "branch '${REF_NAME}' is not in the configured branches list, skipping lint."
    return 0
  fi

  if is_pr_event && [[ "$PR_MODE" == "title" ]]; then
    lint_pr_title_if_present
    finish
    return
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

    lint_message "commit ${sha}" "$message" || true
    linted_count=$((linted_count + 1))
  done < <(collect_commit_shas)

  if is_pr_event && [[ "$PR_MODE" == "smart" ]] && [[ "$linted_count" -eq 0 ]]; then
    echo "no lintable commits left after filters, falling back to PR title"
    lint_pr_title_if_present
    finish
    return
  fi

  if [[ "$linted_count" -eq 0 ]]; then
    echo "no commit messages to lint after filters"
  fi

  finish
}

main "$@"
