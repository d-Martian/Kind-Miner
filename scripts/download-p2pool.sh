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

# SHA256 checksums for v4.15.1 — update when bumping P2POOL_VERSION.
# Kept in sync with internal/autoinstall/deps.json (the runtime pin manifest).
declare -A SUMS=(
  ["linux-amd64"]="efd8b23579774711a5b86743da980e0936b7c220894063296719116d7f9ba254"
  ["linux-arm64"]="90b2481c04d42487178f5169b3dfe7dc46287f3b0fb2c6cf8b5672a682ad855e"
  ["darwin-amd64"]="0e113c9beff21001ded4a15a3ae2f5ce8a151d18457476923026b322113bc1de"
  ["darwin-arm64"]="391c55474c3f08994340df2824a0b452dac8e0d18ee43cf3b361ce80f00dcd5b"
  ["windows-amd64"]="97b4ba97e65d766ecf223694168b5739e65156390707fbf50f9979054cba52d3"
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
