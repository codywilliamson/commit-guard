#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"; version="$(<"$root/VERSION")"; tag="v$version"
[[ "$(git -C "$root" branch --show-current)" == main ]] || { echo 'release must run on main' >&2; exit 1; }; [[ -z "$(git -C "$root" status --porcelain)" ]] || { echo 'working tree is dirty' >&2; exit 1; }
git -C "$root" rev-parse "$tag" >/dev/null 2>&1 && { echo "tag exists: $tag" >&2; exit 1; }; bash "$root/test/test.sh"; node "$root/scripts/build.mjs"; (cd "$root/dist" && sha256sum commit-guard_* > SHA256SUMS)
git -C "$root" tag -a "$tag" -m "$tag"
git -C "$root" push origin "refs/tags/$tag"
echo "tag $tag pushed; the release workflow will publish the verified artifacts"
