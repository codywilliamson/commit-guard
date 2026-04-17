#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_equals() {
  local expected="$1"
  local actual="$2"
  local message="${3:-values differ}"

  if [[ "$expected" != "$actual" ]]; then
    echo "Expected:" >&2
    printf '%s\n' "$expected" >&2
    echo "Actual:" >&2
    printf '%s\n' "$actual" >&2
    fail "$message"
  fi
}

assert_contains() {
  local needle="$1"
  local haystack="$2"
  local message="${3:-missing expected content}"

  if [[ "$haystack" != *"$needle"* ]]; then
    fail "$message: expected to find '$needle'"
  fi
}

make_temp_dir() {
  mktemp -d
}

make_git_repo() {
  local repo_dir="$1"

  git init -q "$repo_dir"
  git -C "$repo_dir" config user.name "Test User"
  git -C "$repo_dir" config user.email "test@example.com"
}

commit_file() {
  local repo_dir="$1"
  local file_name="$2"
  local content="$3"
  local message="$4"
  local author_name="${5:-Test User}"
  local author_email="${6:-test@example.com}"

  printf '%s\n' "$content" > "${repo_dir}/${file_name}"
  git -C "$repo_dir" add "$file_name"
  GIT_AUTHOR_NAME="$author_name" \
  GIT_AUTHOR_EMAIL="$author_email" \
  GIT_COMMITTER_NAME="$author_name" \
  GIT_COMMITTER_EMAIL="$author_email" \
    git -C "$repo_dir" commit -q -m "$message"
}

make_commitlint_stub() {
  local bin_dir="$1"
  local log_file="$2"

  mkdir -p "$bin_dir"
  cat > "${bin_dir}/commitlint" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

message="$(cat)"
printf '%s\n---\n' "$message" >> "$COMMITLINT_LOG"

if [[ "$message" =~ ^(build|chore|ci|docs|feat|fix|perf|refactor|style|test)(\([[:alnum:]./_-]+\))?(!)?:\ .+ ]]; then
  exit 0
fi

exit 1
EOF
  chmod +x "${bin_dir}/commitlint"
}
