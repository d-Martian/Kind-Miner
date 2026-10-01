#!/usr/bin/env bash
# The kind-minerd .deb for this machine's architecture, built in the pinned
# build container — the one recipe both the release workflow and a rebuilder
# run:
#
#   scripts/release-deb.sh VERSION XMRIG P2POOL OUT_DIR
#
# XMRIG is the binary scripts/build-xmrig.sh made (itself reproducible), and
# P2POOL the pinned release binary. kind-minerd is compiled in the container
# and packed with its dpkg-deb and xz, so the .deb's bytes depend on nothing
# from the host. Writes the .deb and its .sha256 to OUT_DIR.
set -euo pipefail

[[ $# -eq 4 ]] || { sed -n '2,12p' "$0" >&2; exit 2; }
VERSION="$1" XMRIG="$2" P2POOL="$3" OUT="$4"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
case "$(uname -m)" in
	x86_64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}"

mkdir -p "${OUT}/in"
install -m755 "$XMRIG" "${OUT}/in/xmrig"
install -m755 "$P2POOL" "${OUT}/in/p2pool"
scripts/in-build-container.sh "$OUT" -- bash -c "
	set -euo pipefail
	KM_TARGET=kind-minerd bash scripts/reproduce.sh '${VERSION}' /out/in/kind-minerd >/dev/null
	scripts/build-deb.sh '${VERSION}' '${ARCH}' /out/in/kind-minerd /out/in/xmrig /out/in/p2pool /out >/dev/null"
rm -rf "${OUT}/in"
DEB="kind-minerd_${VERSION#v}_${ARCH}.deb"
(cd "$OUT" && sha256sum "$DEB" > "${DEB}.sha256")
echo "${OUT}/${DEB}"
