#!/usr/bin/env bash
# Canonical reproducible build for kind-miner. See REPRODUCIBLE.md.
#
# Builds a bit-for-bit reproducible binary by controlling every input that the
# Go toolchain would otherwise pull from the local machine:
#
#   * a pinned stock Go toolchain          (GOTOOLCHAIN)
#   * no per-user `go env` overrides        (GOENV=off — drops e.g. a local
#                                            CGO_LDFLAGS shim that would bake a
#                                            host path into the binary)
#   * a cleared cgo flag set                (no -L/host/paths recorded)
#   * deterministic build flags             (-trimpath, -buildvcs=false,
#                                            -buildid=)
#
# Usage:  scripts/reproduce.sh [version] [output]
#           version   defaults to `git describe`
#           output    defaults to ./kind-miner
#
# Env:    GOTOOLCHAIN   override the pinned toolchain (e.g. =local for a fast,
#                       offline determinism check via `make verify-repro`)
#         SOURCE_DATE_EPOCH  override the commit-derived timestamp
#
# Only the final `sha256sum` line goes to stdout; progress goes to stderr, so
# the hash is easy to capture or diff.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUTPUT="${2:-kind-miner}"

# Pinned stock toolchain. Keep in sync with the `toolchain` line in go.mod.
# Callers may export GOTOOLCHAIN=local to reproduce with the installed Go.
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.0}"

# Hermetic environment.
export GOENV=off                     # ignore ~/.config/go/env entirely
export GOFLAGS=''
export CGO_ENABLED=1                 # Fyne's GLFW/OpenGL driver needs cgo
unset CGO_CFLAGS CGO_CPPFLAGS CGO_CXXFLAGS CGO_LDFLAGS
export LC_ALL=C LANG=C TZ=UTC
umask 022

# A deterministic clock for any timestamped packaging downstream.
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || echo 0)}"

mkdir -p "$(dirname "${OUTPUT}")"

{
  echo "kind-miner reproducible build"
  echo "  version            ${VERSION}"
  echo "  GOTOOLCHAIN        ${GOTOOLCHAIN}"
  echo "  SOURCE_DATE_EPOCH  ${SOURCE_DATE_EPOCH}"
  echo "  output             ${OUTPUT}"
} >&2

go build \
  -trimpath \
  -buildvcs=false \
  -ldflags "-s -w -buildid= -X main.version=${VERSION}" \
  -o "${OUTPUT}" \
  ./cmd/kind-miner

sha256sum "${OUTPUT}"
