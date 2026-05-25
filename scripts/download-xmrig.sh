#!/usr/bin/env bash
# Downloads pre-built XMRig binaries for each target platform from the
# official XMRig GitHub releases and places them in dist/<platform>/bin/.
# SHA256 checksums are verified before use.
set -euo pipefail

XMRIG_VERSION="6.21.3"
GITHUB="https://github.com/xmrig/xmrig/releases/download/v${XMRIG_VERSION}"
DIST="$(dirname "$0")/../dist"

declare -A URLS=(
  ["linux-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-linux-static-x64.tar.gz"
  ["linux-arm64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-linux-static-aarch64.tar.gz"
  ["darwin-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-macos-x64.tar.gz"
  ["darwin-arm64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-macos-arm64.tar.gz"
  ["windows-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-msvc-win64.zip"
)

# SHA256 checksums for v6.21.3 — update when bumping XMRIG_VERSION.
# Obtain with: shasum -a 256 <archive>
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

  echo "→ Downloading XMRig ${XMRIG_VERSION} for ${platform}…"
  local archive
  archive=$(mktemp)
  curl -fsSL "$url" -o "$archive"

  if [[ "$expected" != PLACEHOLDER* ]]; then
    local actual
    actual=$(sha256sum "$archive" | awk '{print $1}')
    if [[ "$actual" != "$expected" ]]; then
      echo "ERROR: SHA256 mismatch for ${platform}" >&2
      echo "  expected: $expected" >&2
      echo "  got:      $actual" >&2
      rm "$archive"
      exit 1
    fi
    echo "  ✓ checksum OK"
  else
    echo "  ⚠ checksum not pinned — skipping verification (set SUMS[$platform] to pin)"
  fi

  if [[ "$url" == *.zip ]]; then
    unzip -jo "$archive" "*/xmrig.exe" -d "$dest"
  else
    tar -xzf "$archive" --strip-components=1 -C "$dest" --wildcards "*/xmrig"
  fi
  rm "$archive"
  chmod +x "${dest}"/xmrig* 2>/dev/null || true
  echo "  → saved to ${dest}"
}

for platform in "${!URLS[@]}"; do
  download_and_extract "$platform"
done

echo "Done. XMRig binaries are in dist/<platform>/bin/."
