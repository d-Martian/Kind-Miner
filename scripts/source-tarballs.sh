#!/usr/bin/env bash
# Makes the source a release must publish for the GPLv3 engines it bundles:
#
#   scripts/source-tarballs.sh VERSION OUT_DIR
#
# The AppImage, the Flatpak and the .deb carry xmrig and p2pool, both GPLv3,
# and shipping their object code means offering their Corresponding Source to
# everyone who gets it. Publishing it beside the binaries, with every
# release, is the simplest way to do that and needs no written offer.
#
# OUT_DIR gets:
#
#   kind-miner-VERSION-xmrig-X-source.tar.gz
#       Everything that makes the xmrig we ship: the pinned upstream archives
#       (xmrig, and the libuv, hwloc and OpenSSL it links statically), our two
#       patches, and the recipe. Unpacked, it rebuilds offline to the same
#       binary: see its README.
#   p2pool_source-vY.tar.xz
#       p2pool's own complete source for the release we ship, unchanged.
#   SHA256SUMS-source
#
# Tor is not here: it is BSD-licensed, with no such obligation, and its
# source is at https://dist.torproject.org/.
#
# The xmrig tarball is reproducible — sorted, owner 0, mtimes and gzip header
# fixed — so it is the same file for the same pins whoever makes it.
set -euo pipefail

[[ $# -eq 2 ]] || { sed -n '2,25p' "$0" >&2; exit 2; }
VERSION="$1" OUT="$2"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
XLOCK="${ROOT}/engines/xmrig/sources.lock"
PLOCK="${ROOT}/engines/p2pool/sources.lock"
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/kind-miner/source-archives"
mkdir -p "$CACHE" "$OUT"
OUT="$(cd "$OUT" && pwd)"

dep_version() {
	awk -v dep="\"$1\"" '$1 == dep":" { f = 1 } f && /"version"/ { gsub(/[",]/, "", $2); print $2; exit }' \
		"${ROOT}/internal/autoinstall/deps.json"
}
XV="$(dep_version xmrig)"
PV="$(dep_version p2pool)"

# fetch NAME SHA URL: the pinned archive, from the cache or the network,
# checked against its pin either way.
fetch() {
	local name="$1" sum="$2" url="$3" file="${CACHE}/$2"
	if [[ ! -f "$file" ]]; then
		echo "fetching ${name}" >&2
		curl -fsSL "$url" -o "$file.part" && mv "$file.part" "$file"
	fi
	if ! echo "${sum}  ${file}" | sha256sum -c --quiet - >&2; then
		echo "SHA256 mismatch for ${name} (${url}); refusing" >&2
		rm -f "$file"
		exit 1
	fi
	echo "$file"
}

# ---- xmrig: the pinned archives, the patches and the recipe ----
EPOCH="$(awk '$1 == "source_date_epoch" { print $2 }' "$XLOCK")"
NAME="kind-miner-${VERSION}-xmrig-${XV}-source"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
D="${STAGE}/${NAME}"
mkdir -p "${D}/upstream"
while read -r name sum url; do
	[[ -z "$name" || "$name" == \#* || "$name" == source_date_epoch ]] && continue
	cp "$(fetch "$name" "$sum" "$url")" "${D}/upstream/${name}.tar.gz"
done < "$XLOCK"
# The same paths as in the repository, so the recipe runs unchanged.
(cd "$ROOT" && cp --parents -r engines/xmrig scripts/build-xmrig.sh internal/autoinstall/deps.json "$D/")
cat > "${D}/README" <<EOF
xmrig ${XV}, as built for kind-miner ${VERSION}
================================================

This is the complete source of the xmrig binary in kind-miner ${VERSION}'s
AppImage, Flatpak and .deb packages: xmrig ${XV} with the two patches in
engines/xmrig/patches, statically linked against the libuv, hwloc and
OpenSSL archives in upstream/. Each archive is checked against the SHA256 in
engines/xmrig/sources.lock before it is used.

xmrig is free software under the GNU General Public License, version 3 or
later (upstream/xmrig.tar.gz, LICENSE). libuv is MIT-licensed, hwloc
BSD-licensed and OpenSSL Apache-2.0-licensed; their licences are in their
archives.

To rebuild, on the architecture you want the binary for, with podman:

    KM_XMRIG_SOURCES=upstream scripts/build-xmrig.sh out

It compiles inside a digest-pinned container with no network and prints the
binary's SHA256, which matches the xmrig in the packages built for the same
architecture.

The patches:

$(for p in "${ROOT}"/engines/xmrig/patches/*.patch; do echo "  $(basename "$p")"; done)
EOF

# Reproducible tar: the recipe's own clock, sorted names, no owners.
find "$D" -type d -exec chmod 755 {} + && find "$D" -type f -exec chmod 644 {} +
chmod 755 "${D}/scripts/build-xmrig.sh" "${D}/engines/xmrig/build.sh"
XTAR="${OUT}/${NAME}.tar.gz"
tar --sort=name --mtime="@${EPOCH}" --owner=0 --group=0 --numeric-owner \
	--pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime \
	-C "$STAGE" -cf - "$NAME" | gzip -9n > "$XTAR"

# ---- p2pool: its own source release, as published ----
have="$(awk '$1 == "p2pool_source" { print $3 }' "$PLOCK" | sed -n 's|.*/v\([0-9.]*\)/.*|\1|p')"
if [[ "$have" != "$PV" ]]; then
	echo "engines/p2pool/sources.lock pins p2pool ${have:-?}, deps.json says ${PV}; move them together" >&2
	exit 1
fi
read -r _ psum purl < <(awk '$1 == "p2pool_source"' "$PLOCK")
PTAR="${OUT}/$(basename "$purl")"
cp "$(fetch p2pool_source "$psum" "$purl")" "$PTAR"

(cd "$OUT" && sha256sum "$(basename "$XTAR")" "$(basename "$PTAR")" > SHA256SUMS-source)
echo "$XTAR"
echo "$PTAR"
