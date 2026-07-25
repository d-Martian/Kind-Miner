#!/usr/bin/env bash
# Build a single-file kind-miner Flatpak bundle from the working tree.
#
# Uses the dev manifest (flatpak/<app-id>.yml), which builds the current source
# with vendored Go modules (offline). The Flatpak *branch* is set to the build
# version so the manifest's `-X main.version=${FLATPAK_BRANCH:-dev}` bakes the
# real version into the binary — `kind-miner --version` then reports it.
#
# Output: dist/kind-miner-<version>-<arch>.flatpak (+ .sha256). Share that file;
# install it with `flatpak install --user kind-miner-*.flatpak`. The embedded
# Flathub repo URL lets the recipient auto-fetch the freedesktop runtime.
#
# Requires: flatpak-builder, and the runtime/sdk the manifest pins
# (org.freedesktop.{Platform,Sdk}//25.08 + the golang SDK extension):
#   flatpak install flathub org.freedesktop.Platform//25.08 \
#       org.freedesktop.Sdk//25.08 org.freedesktop.Sdk.Extension.golang//25.08
#
# Usage: bash scripts/build-flatpak.sh [version]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

APP_ID="io.github.kind_miner.KindMiner"
MANIFEST="flatpak/${APP_ID}.yml"
VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ARCH="$(uname -m)"
OUTPUT="dist/kind-miner-${VERSION}-${ARCH}.flatpak"

# Deterministic clock for any timestamped packaging step (matches the Makefile).
: "${SOURCE_DATE_EPOCH:=$(git log -1 --format=%ct 2>/dev/null || echo 0)}"
export SOURCE_DATE_EPOCH

if ! command -v flatpak-builder &>/dev/null; then
    echo "error: flatpak-builder not found in PATH" >&2
    echo "  Fedora:  sudo dnf install flatpak-builder" >&2
    echo "  Debian:  sudo apt install flatpak-builder" >&2
    exit 1
fi

echo "Building kind-miner Flatpak ${VERSION} for ${ARCH}…"

# The manifest reads main.version from a VERSION file in the source root
# (flatpak-builder doesn't export FLATPAK_BRANCH into the build sandbox). It is
# gitignored and removed on exit, so the working tree is left untouched.
echo "${VERSION}" > VERSION
trap 'rm -f "${REPO_ROOT}/VERSION"' EXIT

# All build state lives under build/ (gitignored, and skipped by the manifest's
# `type: dir` copy — keeps it out of the sandboxed source and avoids recursion).
REPO_DIR="build/flatpak-repo"
BUILD_DIR="build/flatpak-app"
STATE_DIR="build/.flatpak-builder"
rm -rf "${REPO_DIR}" "${BUILD_DIR}"

# --disable-rofiles-fuse: build without FUSE so this also works in containers/CI.
# --default-branch=${VERSION}: feeds FLATPAK_BRANCH -> main.version (see header).
flatpak-builder \
    --force-clean \
    --disable-rofiles-fuse \
    --state-dir="${STATE_DIR}" \
    --repo="${REPO_DIR}" \
    --default-branch="${VERSION}" \
    "${BUILD_DIR}" \
    "${MANIFEST}"

mkdir -p dist
flatpak build-bundle \
    --runtime-repo=https://flathub.org/repo/flathub.flatpakrepo \
    "${REPO_DIR}" \
    "${OUTPUT}" \
    "${APP_ID}" \
    "${VERSION}"

sha256sum "${OUTPUT}" > "${OUTPUT}.sha256"

echo ""
echo "✓ ${OUTPUT}"
echo "✓ ${OUTPUT}.sha256"
cat "${OUTPUT}.sha256"
