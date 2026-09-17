#!/usr/bin/env bash
set -euo pipefail

repo="codywilliamson/commit-guard"
ref="${COMMIT_GUARD_REF:-v0.3.0}"
if [[ ! "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'COMMIT_GUARD_REF must name an exact release, such as v0.3.0.' >&2
  exit 2
fi
version="${ref#v}"
system="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
extension=''
case "$system" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) system=windows; extension=.exe ;;
  *) echo "Unsupported operating system: $system" >&2; exit 2 ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 2 ;;
esac

args=(--ref "$ref")
while (($#)); do
  case "$1" in
    --pr-mode) args+=(--mode "${2:?missing PR mode}"); shift 2 ;;
    --hook-mode)
      case "${2:?missing hook mode}" in
        none) args+=(--ci-only) ;;
        auto|native|husky) ;; # Integrates with existing Husky hooks.
        *) echo "Unknown hook mode: $2" >&2; exit 2 ;;
      esac
      shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done

asset="commit-guard_${version}_${system}_${arch}${extension}"
base="https://github.com/$repo/releases/download/$ref"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 10 --max-time 60 "$base/$asset" -o "$temp_dir/$asset"
curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 10 --max-time 30 "$base/SHA256SUMS" -o "$temp_dir/SHA256SUMS"
expected="$(awk -v name="$asset" '$2 == name { print $1 }' "$temp_dir/SHA256SUMS")"
[[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || { echo "Missing or ambiguous checksum for $asset" >&2; exit 2; }
if command -v sha256sum >/dev/null; then
  actual="$(sha256sum "$temp_dir/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$temp_dir/$asset" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || { echo 'Release checksum verification failed.' >&2; exit 2; }
chmod +x "$temp_dir/$asset"
"$temp_dir/$asset" install "${args[@]}"
