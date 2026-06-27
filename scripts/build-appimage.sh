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
    echo "Download it from: https://github.com/AppImage/appimagetool/releases/tag/continuous"
    echo "  wget -O appimagetool https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage"
    echo "  chmod +x appimagetool && sudo mv appimagetool /usr/local/bin/"
    exit 1
fi

echo "Building kind-miner ${VERSION} for linux/${ARCH}…"

# Build binary. The GUI needs cgo (Fyne's GLFW/OpenGL driver), so the build
# host must have the GL/X11/Wayland dev libraries installed — on Fedora:
#   sudo dnf install libXxf86vm-devel libX11-devel mesa-libGL-devel \
#                    libxkbcommon-devel wayland-devel
CGO_ENABLED=1 go build \
    -trimpath -buildvcs=false \
    -ldflags "-s -w -buildid= -X main.version=${VERSION}" \
    -o "build/kind-miner-linux-${ARCH}" \
    ./cmd/kind-miner

# Assemble AppDir
rm -rf "${APPDIR}"
mkdir -p "${APPDIR}/usr/bin"

cp "build/kind-miner-linux-${ARCH}" "${APPDIR}/usr/bin/kind-miner"
# Top-level AppImage icon. app-512.png is correctly sized for the 512x512
# hicolor slot the AppRun installs into on first run (app.png is 1024x1024).
cp assets/icons/app-512.png "${APPDIR}/kind-miner.png"
cp kind-miner.desktop "${APPDIR}/kind-miner.desktop"

# Bundle the GUI's shared-library dependencies (X11/Wayland/xkbcommon, …) so the
# AppImage runs on systems that lack them. Following AppImage convention we
# deliberately do NOT bundle the OpenGL stack or the C runtime — those must come
# from the host to match its GPU driver and kernel.
mkdir -p "${APPDIR}/usr/lib"
EXCLUDE='libGL|libGLX|libGLdispatch|libEGL|libOpenGL|libdrm|libgbm|libglapi|libc\.so|libm\.so|libdl\.so|libpthread|librt\.so|libresolv|ld-linux|libstdc\+\+|libgcc_s'
ldd "${APPDIR}/usr/bin/kind-miner" \
    | awk '/=> \// {print $3}' \
    | grep -vE "${EXCLUDE}" \
    | while read -r lib; do
        cp -Ln "${lib}" "${APPDIR}/usr/lib/" 2>/dev/null || true
      done
echo "Bundled $(ls -1 "${APPDIR}/usr/lib" 2>/dev/null | wc -l) libraries."

cat > "${APPDIR}/AppRun" << 'APPRUN'
#!/bin/sh
SELF="$(readlink -f "$0")"
HERE="${SELF%/*}"
export PATH="${HERE}/usr/bin:${PATH}"
export LD_LIBRARY_PATH="${HERE}/usr/lib:${LD_LIBRARY_PATH}"

# First-run desktop integration: GNOME (and most desktops without
# AppImageLauncher) won't launch a raw AppImage on double-click, so install a
# launcher + icon pointing back at this AppImage the first time it runs. Skipped
# if already integrated or not running as an AppImage.
if [ -n "${APPIMAGE}" ]; then
    data="${XDG_DATA_HOME:-$HOME/.local/share}"
    desktop="${data}/applications/kind-miner.desktop"
    if [ ! -e "${desktop}" ]; then
        mkdir -p "${data}/applications" "${data}/icons/hicolor/512x512/apps"
        cp "${HERE}/kind-miner.png" "${data}/icons/hicolor/512x512/apps/kind-miner.png" 2>/dev/null || true
        cat > "${desktop}" <<DESK
[Desktop Entry]
Version=1.0
Type=Application
Name=kind-miner
GenericName=Monero Miner
Comment=Idle Monero CPU mining for always-on machines
Exec=${APPIMAGE}
Icon=kind-miner
Categories=Network;
Keywords=monero;mining;cryptocurrency;xmr;p2pool;
Terminal=false
StartupNotify=false
DESK
        chmod +x "${desktop}" 2>/dev/null || true
        command -v update-desktop-database >/dev/null 2>&1 && \
            update-desktop-database "${data}/applications" >/dev/null 2>&1 || true
    fi
fi

exec "${HERE}/usr/bin/kind-miner" "$@"
APPRUN
chmod +x "${APPDIR}/AppRun"

# Build AppImage
ARCH="${ARCH}" appimagetool "${APPDIR}" "${OUTPUT}"
sha256sum "${OUTPUT}" > "${OUTPUT}.sha256"

echo ""
echo "✓ ${OUTPUT}"
echo "✓ ${OUTPUT}.sha256"
