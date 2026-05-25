#!/usr/bin/env bash
# Downloads pre-built p2pool binaries for each target platform.
set -euo pipefail

P2POOL_VERSION="4.15.1"
GITHUB="https://github.com/SChernykh/p2pool/releases/download/v${P2POOL_VERSION}"
DIST="$(dirname "$0")/../dist"

declare -A URLS=(
  ["linux-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-linux-x64.tar.gz"
  ["linux-arm64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-linux-aarch64.tar.gz"
  ["darwin-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-macos-x64.tar.gz"
  ["darwin-arm64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-macos-aarch64.tar.gz"
  ["windows-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-windows-x64.zip"
)

declare -A SUMS=(
  ["linux-amd64"]="PLACEHOLDER_UPDATE_WHEN_PINNING_VERSION"
  ["linux-arm64"]="PLACEHOLDER_UPDATE_WHEN_PINNING_VERSION"
  ["darwin-amd64"]="PLACEHOLDER_UPDATE_WHEN_PINNING_VERSION"
  ["darwin-arm64"]="PLACEHOLDER_UPDATE_WHEN_PINNING_VERSION"
  ["windows-amd64"]="PLACEHOLDER_UPDATE_WHEN_PINNING_VERSION"
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
