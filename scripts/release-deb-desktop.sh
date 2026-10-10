#!/usr/bin/env bash
# The kind-miner desktop .deb, built in the pinned build container — the one
# recipe both the release workflow and a rebuilder run:
#
#   scripts/release-deb-desktop.sh VERSION ENGINES OUT_DIR
#
# ENGINES is the directory tools/stage-engines made (staged on the host,
# because the container builds with the network off). The GUI is compiled in
# the container, exactly as for the release tarball, and packed with its
# dpkg-deb and xz. Writes the .deb and its .sha256 to OUT_DIR.
set -euo pipefail

[[ $# -eq 3 ]] || { sed -n '2,10p' "$0" >&2; exit 2; }
VERSION="$1" ENGINES="$2" OUT="$3"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[[ "$(uname -m)" == x86_64 ]] || { echo "the desktop .deb is amd64; build it on x86_64" >&2; exit 1; }
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}"

rm -rf "${OUT}/in" && mkdir -p "${OUT}/in"
cp -R "$ENGINES" "${OUT}/in/engines"
scripts/in-build-container.sh "$OUT" -- bash -c "
	set -euo pipefail
	bash scripts/reproduce.sh '${VERSION}' /out/in/kind-miner >/dev/null
	scripts/build-deb-desktop.sh '${VERSION}' amd64 /out/in/kind-miner /out/in/engines /out >/dev/null"
rm -rf "${OUT}/in"
DEB="kind-miner_${VERSION#v}_amd64.deb"
(cd "$OUT" && sha256sum "$DEB" > "${DEB}.sha256")
echo "${OUT}/${DEB}"
