#!/usr/bin/env bash
# Build a reproducible vendored source tarball for the Flatpak/Flathub build.
#
# Flathub builds run offline, so the Go modules must travel with the source.
# Rather than commit a vendor/ tree (kept gitignored here) or pull a
# third-party source generator, we ship the committed source plus `go mod
# vendor` output as a single deterministic tarball that the Flathub manifest
# consumes via `type: archive`. The build there uses -mod=vendor (no network).
#
# Determinism: contents come from `git archive HEAD` (so the tree matches the
# commit) plus vendored modules (fixed by go.mod/go.sum); tar metadata is
# normalised (sorted names, fixed mtime, uid/gid 0) and gzip drops its
# timestamp. The same commit therefore yields a byte-identical tarball, so the
# sha256 pinned in the manifest stays valid.
#
# Usage: bash scripts/vendor-tarball.sh [version]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
PREFIX="kind-miner-${VERSION}"
OUTDIR="dist"
OUTPUT="${OUTDIR}/${PREFIX}-vendored-src.tar.gz"

# Deterministic clock: the commit time (matches the Makefile).
: "${SOURCE_DATE_EPOCH:=$(git log -1 --format=%ct 2>/dev/null || echo 0)}"
export SOURCE_DATE_EPOCH

if ! git diff --quiet HEAD 2>/dev/null; then
    echo "warning: working tree is dirty — tarball reflects HEAD, not local edits" >&2
fi

echo "Building vendored source tarball ${VERSION}…"

STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

# Committed source at HEAD (respects .gitignore — no build/, bin/, etc.).
mkdir -p "${STAGE}/${PREFIX}"
git archive HEAD | tar -x -C "${STAGE}/${PREFIX}"

# Vendored modules (gitignored, so not in the archive above).
go mod vendor
cp -a vendor "${STAGE}/${PREFIX}/vendor"

mkdir -p "${OUTDIR}"
# Normalise every source of nondeterminism: stable sort, fixed mtime/owner,
# no pax atime/ctime headers, and gzip without its mtime byte. The recipe lives
# in scripts/package.sh so this script, the Makefile, and CI share one copy.
bash "$(dirname "$0")/package.sh" tar "${OUTPUT}" "${STAGE}" "${PREFIX}"
bash "$(dirname "$0")/package.sh" sha256 "${OUTPUT}"

echo ""
echo "✓ ${OUTPUT}"
echo "✓ ${OUTPUT}.sha256"
cat "${OUTPUT}.sha256"
