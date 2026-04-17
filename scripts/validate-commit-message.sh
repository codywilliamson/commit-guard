#!/usr/bin/env bash
set -euo pipefail

MESSAGE_FILE="${1:-}"

if [[ -z "$MESSAGE_FILE" || ! -f "$MESSAGE_FILE" ]]; then
  echo "error: commit-msg hook requires a commit message file path." >&2
  exit 1
fi

SUBJECT="$(head -n 1 "$MESSAGE_FILE")"
CONVENTIONAL_REGEX='^(build|chore|ci|docs|feat|fix|perf|refactor|style|test)(\([[:alnum:]./_-]+\))?(!)?: .+'

if [[ "$SUBJECT" =~ ^Merge[[:space:]] ]] || \
   [[ "$SUBJECT" =~ ^Revert[[:space:]] ]] || \
   [[ "$SUBJECT" =~ ^fixup!\  ]] || \
   [[ "$SUBJECT" =~ ^squash!\  ]]; then
  exit 0
fi

if [[ "$SUBJECT" =~ $CONVENTIONAL_REGEX ]]; then
  exit 0
fi

cat >&2 <<EOF
error: commit message must follow Conventional Commits.

Expected:
  type(scope): description

Example:
  feat(ci): add smart PR title fallback

Received:
  ${SUBJECT}
EOF

exit 1
