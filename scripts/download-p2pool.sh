#!/usr/bin/env bash
# Downloads pre-built p2pool binaries for each target platform.
set -euo pipefail

P2POOL_VERSION="4.18"
GITHUB="https://github.com/SChernykh/p2pool/releases/download/v${P2POOL_VERSION}"
DIST="$(dirname "$0")/../dist"

declare -A URLS=(
  ["linux-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-linux-x64.tar.gz"
  ["linux-arm64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-linux-aarch64.tar.gz"
  ["darwin-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-macos-x64.tar.gz"
  ["darwin-arm64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-macos-aarch64.tar.gz"
  ["windows-amd64"]="${GITHUB}/p2pool-v${P2POOL_VERSION}-windows-x64.zip"
)

# SHA256 checksums for v4.18 — update when bumping P2POOL_VERSION.
# Kept in sync with internal/autoinstall/deps.json (the runtime pin manifest).
declare -A SUMS=(
  ["linux-amd64"]="893691726b0218fe1883a7a326e2c69db4eb228fc72ba00c8adfa6be85b8a415"
  ["linux-arm64"]="da189a52c11d274112fb2c26495ba15b748b8504d278d8f8d16f3d8674747bb2"
  ["darwin-amd64"]="a62be84b6ca4e4e980ab4b1785a6bc191d5eed15621f0777d3e91008457e8532"
  ["darwin-arm64"]="b9b6abae4380fb3adde0696e5a8edc40f88783f685e210668b9386f07ddba856"
  ["windows-amd64"]="36e53c383535c29222dfdbcd8d2bb372c610309f9a3f9435e99783431be830d4"
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
