#!/usr/bin/env bash
# Runs a command in the pinned build container (packaging/buildenv/Containerfile),
# with the repository mounted read-only at /src and OUT_DIR at /out:
#
#   scripts/in-build-container.sh OUT_DIR -- COMMAND...
#
# The Go modules are downloaded first, with the network on, into a cache kept
# beside OUT_DIR; go.sum checks every one. The command itself then runs with
# the network off, so a build can use nothing it was not pinned to.
#
# Needs podman. The image's tag is a hash of the Containerfile, so a changed
# pin never reuses a stale image.
set -euo pipefail

[[ $# -ge 3 && "$2" == "--" ]] || { sed -n '2,12p' "$0" >&2; exit 2; }
OUT="$(mkdir -p "$1" && cd "$1" && pwd)"
shift 2

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FILE="${ROOT}/packaging/buildenv/Containerfile"
tag="kind-miner-build:$(sha256sum "$FILE" | cut -c1-12)"
if ! podman image exists "$tag"; then
	echo "building ${tag}" >&2
	podman build --quiet -t "$tag" -f "$FILE" "${ROOT}/packaging/buildenv" >&2
fi

MODS="${KM_GOMODCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/kind-miner/gomodcache}"
mkdir -p "$MODS"
podman run --rm -v "${ROOT}:/src:ro,z" -v "${MODS}:/gomod:z" -e GOMODCACHE=/gomod -e GOPROXY=https://proxy.golang.org \
	-w /src "$tag" go mod download >&2

podman run --rm --network=none \
	-v "${ROOT}:/src:ro,z" -v "${MODS}:/gomod:ro,z" -v "${OUT}:/out:z" \
	-e GOMODCACHE=/gomod -e GOCACHE=/tmp/gocache \
	${SOURCE_DATE_EPOCH:+-e SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH"} \
	-w /src "$tag" "$@"
