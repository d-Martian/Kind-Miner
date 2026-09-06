#!/usr/bin/env bash
# Downloads pre-built XMRig binaries for each target platform from the
# official XMRig GitHub releases and places them in dist/<platform>/bin/.
# SHA256 checksums are verified before use.
set -euo pipefail

XMRIG_VERSION="6.26.0"
GITHUB="https://github.com/xmrig/xmrig/releases/download/v${XMRIG_VERSION}"
DIST="$(dirname "$0")/../dist"

# XMRig publishes no Linux ARM64 prebuilt, so that platform is not packaged.
declare -A URLS=(
  ["linux-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-linux-static-x64.tar.gz"
  ["darwin-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-macos-x64.tar.gz"
  ["darwin-arm64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-macos-arm64.tar.gz"
  ["windows-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-windows-x64.zip"
)

# SHA256 checksums for v6.26.0 — update when bumping XMRIG_VERSION.
# Obtain with: shasum -a 256 <archive>. Kept in sync with
# internal/autoinstall/deps.json (the runtime pin manifest).
declare -A SUMS=(
  ["linux-amd64"]="fc6f8ae5f64e4f17481f7e3be29a1c56949f216a998414188003eae1db20c9e5"
  ["darwin-amd64"]="1da924b358c0089e361540c4a9e6f8b09538b29efeafa2379590e0f6db358ff4"
  ["darwin-arm64"]="6ae4eb4216e99a201ae9a3d2c3a7c275207c5165cfc25da1f3d735d6c4829c18"
  ["windows-amd64"]="bba8097cb37d9b458a1cb1137876b27cde6740d17fe4ccbc086ba07d87d9e147"
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
