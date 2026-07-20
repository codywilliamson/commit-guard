#!/usr/bin/env bash
set -euo pipefail

# commit-guard native commit-msg hook
# reads optional .commit-guard.json at the repo root
# config parsing is duplicated in run-commitlint-ci.sh — keep in sync

MESSAGE_FILE="${1:-}"

if [[ -z "$MESSAGE_FILE" || ! -f "$MESSAGE_FILE" ]]; then
  echo "error: commit-msg hook requires a commit message file path." >&2
  exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
CONFIG_FILE="${REPO_ROOT}/.commit-guard.json"

DEFAULT_TYPES='build|chore|ci|docs|feat|fix|perf|refactor|style|test'
AI_ATTRIBUTION_PATTERNS=(
  '^co-authored-by:.*(claude|copilot|chatgpt|openai|anthropic|gemini|cursor|devin|aider|codex|\[bot\])'
  'generated (with|by).*(claude|chatgpt|copilot|gemini|cursor|aider|codex)'
  'noreply@anthropic\.com'
)

have_jq() {
  [[ -z "${CG_FORCE_FALLBACK_PARSER:-}" ]] && command -v jq >/dev/null 2>&1
}

if [[ -f "$CONFIG_FILE" ]] && have_jq && ! jq empty "$CONFIG_FILE" 2>/dev/null; then
  echo "error: ${CONFIG_FILE} is not valid JSON." >&2
  exit 1
fi

config_get() {
  local key="$1"
  local default="$2"
  local value=""

  if [[ -f "$CONFIG_FILE" ]]; then
    if have_jq; then
      value="$(jq -r --arg k "$key" 'if has($k) then .[$k] | tostring else "" end' "$CONFIG_FILE")"
    else
      value="$(sed -n 's/^[[:space:]]*"'"$key"'"[[:space:]]*:[[:space:]]*"\{0,1\}\([^",]*\)"\{0,1\},\{0,1\}[[:space:]]*$/\1/p' "$CONFIG_FILE" | head -n 1)"
    fi
  fi

  printf '%s\n' "${value:-$default}"
}

config_get_array() {
  local key="$1"

  if [[ ! -f "$CONFIG_FILE" ]]; then
    return 0
  fi

  if have_jq; then
    jq -r --arg k "$key" '.[$k] // [] | .[]' "$CONFIG_FILE"
  else
    # fallback: flat pretty-printed json, one array element per line
    awk -v key="\"${key}\"" '
      index($0, key) && /\[/ { inside = 1; next }
      inside && /\]/ { exit }
      inside {
        gsub(/^[[:space:]]*"?/, "")
        gsub(/"?,?[[:space:]]*$/, "")
        if (length($0)) print
      }
    ' "$CONFIG_FILE"
  fi
}

ENFORCE="$(config_get enforce block)"
AI_ATTRIBUTION="$(config_get ai-attribution allow)"

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

TYPES="$(config_get_array types | paste -sd '|' -)"
TYPES="${TYPES:-$DEFAULT_TYPES}"
CONVENTIONAL_REGEX="^(${TYPES})(\([[:alnum:]./_-]+\))?(!)?: .+"

VIOLATIONS=""

add_violation() {
  VIOLATIONS="${VIOLATIONS}${1}"$'\n'
}

find_ai_attribution_lines() {
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
  done < "$MESSAGE_FILE"
}

strip_ai_attribution_lines() {
  local temp_file
  local line
  local lowered
  local pattern
  local keep

  temp_file="$(mktemp)"
  while IFS= read -r line; do
    keep=true
    lowered="${line,,}"
    for pattern in "${AI_ATTRIBUTION_PATTERNS[@]}"; do
      if [[ "$lowered" =~ $pattern ]]; then
        keep=false
        break
      fi
    done
    if [[ "$keep" == true ]]; then
      printf '%s\n' "$line" >> "$temp_file"
    fi
  done < "$MESSAGE_FILE"
  mv "$temp_file" "$MESSAGE_FILE"
}

check_ai_attribution() {
  local matches

  if [[ "$AI_ATTRIBUTION" == "allow" ]]; then
    return 0
  fi

  matches="$(find_ai_attribution_lines)"
  if [[ -z "$matches" ]]; then
    return 0
  fi

  case "$AI_ATTRIBUTION" in
    warn)
      echo "warning: commit message contains AI attribution:" >&2
      printf '%s\n' "$matches" >&2
      ;;
    strip)
      strip_ai_attribution_lines
      echo "commit-guard: stripped AI attribution from commit message:" >&2
      printf '%s\n' "$matches" >&2
      ;;
    block)
      add_violation "commit message contains AI attribution:"$'\n'"$matches"
      ;;
  esac
}

check_ban_patterns() {
  local pattern
  local message

  message="$(cat "$MESSAGE_FILE")"
  while IFS= read -r pattern; do
    if [[ -z "$pattern" ]]; then
      continue
    fi
    if grep -Eiq -- "$pattern" <<< "$message"; then
      add_violation "commit message matches banned pattern: ${pattern}"
    fi
  done < <(config_get_array ban-patterns)
}

check_conventional_subject() {
  local subject

  subject="$(head -n 1 "$MESSAGE_FILE")"

  if [[ "$subject" =~ ^Merge[[:space:]] ]] || \
     [[ "$subject" =~ ^Revert[[:space:]] ]] || \
     [[ "$subject" =~ ^fixup!\  ]] || \
     [[ "$subject" =~ ^squash!\  ]]; then
    return 0
  fi

  if [[ "$subject" =~ $CONVENTIONAL_REGEX ]]; then
    return 0
  fi

  add_violation "commit message must follow Conventional Commits.

Expected:
  type(scope): description

Allowed types:
  $(printf '%s' "$TYPES" | tr '|' ' ')

Received:
  ${subject}"
}

check_ai_attribution
check_ban_patterns
check_conventional_subject

if [[ -z "$VIOLATIONS" ]]; then
  exit 0
fi

printf 'error: %s\n' "$VIOLATIONS" >&2

if [[ "$ENFORCE" == "warn" ]]; then
  echo "commit-guard: enforce is 'warn', allowing commit anyway." >&2
  exit 0
fi

exit 1
