#!/usr/bin/env bash
# Downloads pre-built p2pool binaries for each target platform.
set -euo pipefail

P2POOL_VERSION="4.18.1"
GITHUB="https://github.com/SChernykh/p2pool/releases/download/v${P2POOL_VERSION}"
DIST="$(dirname "$0")/../dist"

declare -A URLS=(
  ["linux-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-linux-x64.tar.gz"
  ["linux-arm64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-linux-aarch64.tar.gz"
  ["darwin-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-macos-x64.tar.gz"
  ["darwin-arm64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-macos-aarch64.tar.gz"
  ["windows-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-windows-x64.zip"
)

# SHA256 checksums for v4.18.1 — update when bumping P2POOL_VERSION.
# Kept in sync with internal/autoinstall/deps.json (the runtime pin manifest).
declare -A SUMS=(
  ["linux-amd64"]="eeab5aca0edf4756cb295c5fda5b2d5344208aecbbdcb2ffd8471dfe14e6c2c5"
  ["linux-arm64"]="7e876d34f9fbd5fd8e39431e7185af23b58b5a5ad2c2847f33724592880a53d6"
  ["darwin-amd64"]="bbb9872724aa30305d6b49346a52795d375c1bae2e31e45cf5d3f37fa53342c5"
  ["darwin-arm64"]="033fca6a0292e850c8807b93bc0ac39014d788b93ed4c5ea56ed09c949511c18"
  ["windows-amd64"]="257094eb827aca2c08801775028c141e99966c3c640eb42ec094cf540fe588cc"
)

download_and_extract() {
  local platform="$1"
  local url="${URLS[$platform]}"
  local expected="${SUMS[$platform]}"
  local dest="${DIST}/${platform}/bin"
  mkdir -p "$dest"

  echo "→ Downloading p2pool ${P2POOL_VERSION} for ${platform}…"
  local archive
  archive=$(mktemp)
  curl -fsSL "$url" -o "$archive"

  if [[ "$expected" != PLACEHOLDER* ]]; then
    local actual
    actual=$(sha256sum "$archive" | awk '{print $1}')
    if [[ "$actual" != "$expected" ]]; then
      echo "ERROR: SHA256 mismatch for ${platform}" >&2
      rm "$archive"
      exit 1
    fi
    echo "  ✓ checksum OK"
  else
    echo "  ⚠ checksum not pinned — skipping verification"
  fi

  if [[ "$url" == *.zip ]]; then
    unzip -jo "$archive" "*/p2pool.exe" -d "$dest"
  else
    tar -xzf "$archive" --strip-components=1 -C "$dest" --wildcards "*/p2pool"
  fi
  rm "$archive"
  chmod +x "${dest}"/p2pool* 2>/dev/null || true
  echo "  → saved to ${dest}"
}

for platform in "${!URLS[@]}"; do
  download_and_extract "$platform"
done

echo "Done. p2pool binaries are in dist/<platform>/bin/."
