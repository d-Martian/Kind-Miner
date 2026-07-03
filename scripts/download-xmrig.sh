#!/usr/bin/env bash
# Downloads pre-built XMRig binaries for each target platform from the
# official XMRig GitHub releases and places them in dist/<platform>/bin/.
# SHA256 checksums are verified before use.
set -euo pipefail

XMRIG_VERSION="6.21.3"
GITHUB="https://github.com/xmrig/xmrig/releases/download/v${XMRIG_VERSION}"
DIST="$(dirname "$0")/../dist"

# XMRig publishes no Linux ARM64 prebuilt, so that platform is not packaged.
declare -A URLS=(
  ["linux-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-linux-static-x64.tar.gz"
  ["darwin-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-macos-x64.tar.gz"
  ["darwin-arm64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-macos-arm64.tar.gz"
  ["windows-amd64"]="${GITHUB}/xmrig-${XMRIG_VERSION}-msvc-win64.zip"
)

# SHA256 checksums for v6.21.3 — update when bumping XMRIG_VERSION.
# Obtain with: shasum -a 256 <archive>. Kept in sync with
# internal/autoinstall/deps.json (the runtime pin manifest).
declare -A SUMS=(
  ["linux-amd64"]="a0eefd7a5c0efd1cac153a075b4fdead443a04f11cc587a09bd5ac09e174f10f"
  ["darwin-amd64"]="4f6c7aa6d5d8ffa1429021db6d6104f42c2691abbab2e01d123356192bcf06fa"
  ["darwin-arm64"]="d7badde96309772bd219503bce91a239ed83dae042d426ef7aa663fce007dccf"
  ["windows-amd64"]="713263085499ae626a6148fab67932c9a69611b21ac3d04cf52a5e23495f902e"
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
