#!/usr/bin/env bash
# Build a kind-miner AppImage for the current architecture.
# Requires: appimagetool (https://github.com/AppImage/AppImageKit/releases)
# Usage: bash scripts/build-appimage.sh [version]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ARCH="$(uname -m)"
APPDIR="build/appdir"
OUTPUT="kind-miner-${VERSION}-linux-${ARCH}.AppImage"

if ! command -v appimagetool &>/dev/null; then
    echo "error: appimagetool not found in PATH"
    echo "Download it from: https://github.com/AppImage/AppImageKit/releases"
    echo "  wget -O appimagetool https://github.com/AppImage/AppImageKit/releases/latest/download/appimagetool-x86_64.AppImage"
    echo "  chmod +x appimagetool && sudo mv appimagetool /usr/local/bin/"
    exit 1
fi

echo "Building kind-miner ${VERSION} for linux/${ARCH}…"

# Build binary
CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o "build/kind-miner-linux-${ARCH}" \
    ./cmd/kind-miner

# Assemble AppDir
rm -rf "${APPDIR}"
mkdir -p "${APPDIR}/usr/bin"

cp "build/kind-miner-linux-${ARCH}" "${APPDIR}/usr/bin/kind-miner"
cp assets/icons/app.png "${APPDIR}/kind-miner.png"
cp kind-miner.desktop "${APPDIR}/kind-miner.desktop"

cat > "${APPDIR}/AppRun" << 'APPRUN'
#!/bin/sh
SELF="$(readlink -f "$0")"
HERE="${SELF%/*}"
export PATH="${HERE}/usr/bin:${PATH}"
exec "${HERE}/usr/bin/kind-miner" "$@"
APPRUN
chmod +x "${APPDIR}/AppRun"

# Build AppImage
ARCH="${ARCH}" appimagetool "${APPDIR}" "${OUTPUT}"
sha256sum "${OUTPUT}" > "${OUTPUT}.sha256"

echo ""
echo "✓ ${OUTPUT}"
echo "✓ ${OUTPUT}.sha256"
