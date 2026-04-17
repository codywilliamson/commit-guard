#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="${ROOT_DIR}/VERSION"
CHANGELOG_FILE="${ROOT_DIR}/CHANGELOG.md"
VERSION="${1:-$(cat "$VERSION_FILE")}"
TAG="v${VERSION}"

extract_release_notes() {
  awk -v version="$VERSION" '
    $0 == "## [" version "]" { capture = 1; next }
    capture && /^## \[/ { exit }
    capture { print }
  ' "$CHANGELOG_FILE" | sed '/^[[:space:]]*$/d'
}

if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "error: git tag ${TAG} already exists." >&2
  exit 1
fi

if gh release view "$TAG" >/dev/null 2>&1; then
  echo "error: GitHub release ${TAG} already exists." >&2
  exit 1
fi

NOTES="$(extract_release_notes)"
if [[ -z "$NOTES" ]]; then
  NOTES="Release ${TAG}"
fi

git tag -a "$TAG" -m "$TAG"
git push origin "refs/tags/${TAG}"
gh release create "$TAG" --verify-tag --title "$TAG" --notes "$NOTES"
