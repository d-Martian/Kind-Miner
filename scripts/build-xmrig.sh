#!/usr/bin/env bash
# Builds xmrig from source for this machine's architecture (x86_64 or aarch64),
# reproducibly, with kind-miner's two patches. See engines/xmrig/ and
# REPRODUCIBLE.md.
#
#   1. Fetch every source archive in engines/xmrig/sources.lock and check its
#      SHA256. Nothing unpinned goes further.
#   2. Build the toolchain image (engines/xmrig/Containerfile: a digest-pinned
#      Alpine with exact package versions).
#   3. Compile inside it with the sources and patches mounted read-only and the
#      network off, writing the binary to the output directory.
#
# Usage:  scripts/build-xmrig.sh [output-dir]
#           output-dir  defaults to dist/xmrig/linux-<arch>
#
# KM_XMRIG_SOURCES=<dir> takes the pinned archives from <dir>/<name>.tar.gz
# instead of downloading them — how the source tarball published with each
# release (scripts/source-tarballs.sh) rebuilds offline.
#
# Only the final sha256sum line goes to stdout, like scripts/reproduce.sh, so
# two builds are easy to compare.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENGINE="${REPO_ROOT}/engines/xmrig"
LOCK="${ENGINE}/sources.lock"

case "$(uname -m)" in
  x86_64)        ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
OUT="$(mkdir -p "${1:-${REPO_ROOT}/dist/xmrig/linux-${ARCH}}" && cd "${1:-${REPO_ROOT}/dist/xmrig/linux-${ARCH}}" && pwd)"
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/kind-miner/xmrig-sources"
mkdir -p "$CACHE"

# The version lives in deps.json; the source pin must be for the same one.
want="$(sed -n '/"xmrig"/,/"version"/s/.*"version": *"\([^"]*\)".*/\1/p' "${REPO_ROOT}/internal/autoinstall/deps.json" | head -1)"
have="$(awk '$1 == "xmrig" { print $3 }' "$LOCK" | sed -n 's|.*/v\([0-9.]*\)\.tar\.gz|\1|p')"
if [[ -z "$want" || "$want" != "$have" ]]; then
  echo "engines/xmrig/sources.lock pins xmrig ${have:-?}, deps.json says ${want:-?}; bump them together" >&2
  exit 1
fi

# 1. Pinned sources. A fresh temp dir holds exactly the pinned archives, so the
#    container sees nothing else from the cache.
SRC="$(mktemp -d)"
trap 'rm -rf "$SRC"' EXIT
while read -r name sum url; do
  [[ -z "$name" || "$name" == \#* || "$name" == source_date_epoch ]] && continue
  file="$CACHE/${sum}.tar.gz"
  if [[ -n "${KM_XMRIG_SOURCES:-}" ]]; then
    # Rebuilding from a published source tarball: its archives, offline.
    # They are checked against the pins all the same.
    file="${KM_XMRIG_SOURCES}/${name}.tar.gz"
  elif [[ ! -f "$file" ]]; then
    echo "fetching ${name}" >&2
    curl -fsSL "$url" -o "$file.part" && mv "$file.part" "$file"
  fi
  if ! echo "${sum}  ${file}" | sha256sum -c --quiet - >&2; then
    echo "SHA256 mismatch for ${name} (${url}); refusing to build" >&2
    [[ -z "${KM_XMRIG_SOURCES:-}" ]] && rm -f "$file"
    exit 1
  fi
  cp "$file" "${SRC}/${name}.tar.gz"
done < "$LOCK"

EPOCH="$(awk '$1 == "source_date_epoch" { print $2 }' "$LOCK")"

# 2. Toolchain image. Its tag names what went into it, so a changed
#    Containerfile or build.sh never reuses a stale image.
tag="kind-miner-xmrig-builder:$(cat "${ENGINE}/Containerfile" "${ENGINE}/build.sh" | sha256sum | cut -c1-12)"
if ! podman image exists "$tag"; then
  echo "building toolchain image ${tag}" >&2
  podman build --quiet -t "$tag" "$ENGINE" >&2
fi

# 3. Compile with no network.
{
  echo "xmrig ${want} for linux-${ARCH}"
  echo "  SOURCE_DATE_EPOCH  ${EPOCH}"
  echo "  output             ${OUT}/xmrig"
} >&2
podman run --rm --network=none \
  -e SOURCE_DATE_EPOCH="$EPOCH" \
  -v "${SRC}:/src:ro,Z" \
  -v "${ENGINE}/patches:/patches:ro,Z" \
  -v "${OUT}:/out:Z" \
  "$tag" build-xmrig >&2

sha256sum "${OUT}/xmrig"
