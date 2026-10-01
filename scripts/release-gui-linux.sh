#!/usr/bin/env bash
# The linux-amd64 GUI release archive, built entirely in the pinned build
# container — the one recipe both the release workflow and a rebuilder run:
#
#   scripts/release-gui-linux.sh VERSION OUT_DIR
#
# writes OUT_DIR/kind-miner-VERSION-linux-amd64.tar.gz and its .sha256. The
# binary is cgo (Fyne, GLFW, AppIndicator), so its bytes depend on the C
# compiler and headers; the container pins both, which is what lets anyone
# rebuild the release archive bit for bit.
set -euo pipefail

[[ $# -eq 2 ]] || { sed -n '2,11p' "$0" >&2; exit 2; }
VERSION="$1" OUT="$2"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[[ "$(uname -m)" == x86_64 ]] || { echo "the GUI release archive is linux-amd64; build it on x86_64" >&2; exit 1; }
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}"
NAME="kind-miner-${VERSION}-linux-amd64"

scripts/in-build-container.sh "$OUT" -- bash -c "
	set -euo pipefail
	rm -rf '/out/${NAME}' && mkdir -p '/out/${NAME}'
	bash scripts/reproduce.sh '${VERSION}' '/out/${NAME}/kind-miner' >/dev/null
	cp README.md config.example.yaml '/out/${NAME}/'
	bash scripts/package.sh tar '/out/${NAME}.tar.gz' /out '${NAME}'
	rm -rf '/out/${NAME}'"
(cd "$OUT" && sha256sum "${NAME}.tar.gz" > "${NAME}.tar.gz.sha256")
echo "${OUT}/${NAME}.tar.gz"
