#!/usr/bin/env bash
# Builds the kind-minerd .deb for one architecture, reproducibly: the same
# inputs and SOURCE_DATE_EPOCH give the same file, byte for byte.
#
#   scripts/build-deb.sh VERSION ARCH KIND_MINERD XMRIG P2POOL OUT_DIR
#
#   VERSION      v1.2.3 or 1.2.3
#   ARCH         amd64 | arm64 (Debian's names)
#   KIND_MINERD  the daemon, built with the release flags (make build-minerd)
#   XMRIG        xmrig built from source (scripts/build-xmrig.sh) — never the
#                upstream binary, which keeps a 1% donation outside Tor
#   P2POOL       p2pool from the pinned release (scripts/download-p2pool.sh)
#
# The repository's public key comes from packaging/deb/kind-miner.asc, made
# once by scripts/make-apt-key.sh. Prints the .deb's path.
#
# Layout (files only in /opt/kind-miner, /etc/apt, /usr/lib/systemd/system,
# /usr/share; state in /var/lib/kind-miner, made by systemd):
#   /opt/kind-miner/kind-minerd
#   /opt/kind-miner/engines/{xmrig,p2pool,engines.json}
#   /usr/lib/systemd/system/{kind-minerd.service,kind-miner.slice,
#                             kind-minerd-upgrade.service,kind-minerd-upgrade.timer}
#   /etc/apt/sources.list.d/kind-miner.list, /etc/apt/preferences.d/kind-miner
#   /usr/share/keyrings/kind-miner.asc
set -euo pipefail

[[ $# -eq 6 ]] || { sed -n '2,15p' "$0" >&2; exit 2; }
VERSION="${1#v}" ARCH="$2" KMD="$3" XMRIG="$4" P2POOL="$5" OUT="$6"
: "${SOURCE_DATE_EPOCH:?set SOURCE_DATE_EPOCH (make passes the commit time)}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="${ROOT}/packaging"
KEY="${KM_APT_KEY:-${PKG}/deb/kind-miner.asc}" # the override is for tests
[[ -f "$KEY" ]] || { echo "no repository key at $KEY: run scripts/make-apt-key.sh first" >&2; exit 1; }
case "$ARCH" in amd64|arm64) ;; *) echo "ARCH must be amd64 or arm64" >&2; exit 2 ;; esac
[[ "$VERSION" =~ ^[0-9][0-9A-Za-z.+~-]*$ ]] || { echo "not a Debian version: $VERSION" >&2; exit 2; }
# A pre-release tag (v0.2.0-rc1) becomes 0.2.0~rc1 inside the package. To
# dpkg, "-rc1" is a Debian revision and sorts *after* 0.2.0, so a Nodo on the
# release candidate would never upgrade to the release; "~" sorts before
# anything, which is the Debian convention for exactly this. The file name
# keeps the tag's spelling: GitHub may rename a "~" in an asset name, and a
# rebuilder matches files by name.
DEB_VERSION="${VERSION/-/\~}"

# The versions the engines claim, from the pins they were built against.
dep_version() {
	awk -v dep="\"$1\"" '$1 == dep":" { f = 1 } f && /"version"/ { gsub(/[",]/, "", $2); print $2; exit }' \
		"${ROOT}/internal/autoinstall/deps.json"
}
XMRIG_VERSION="$(dep_version xmrig)"
P2POOL_VERSION="$(dep_version p2pool)"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
D="${STAGE}/kind-minerd"

install -Dm755 "$KMD" "${D}/opt/kind-miner/kind-minerd"
install -Dm755 "$XMRIG" "${D}/opt/kind-miner/engines/xmrig"
install -Dm755 "$P2POOL" "${D}/opt/kind-miner/engines/p2pool"
cat > "${D}/opt/kind-miner/engines/engines.json" <<EOF
{
  "xmrig": "${XMRIG_VERSION}",
  "p2pool": "${P2POOL_VERSION}",
  "xmrig_sha256": "$(sha256sum "$XMRIG" | cut -d' ' -f1)",
  "p2pool_sha256": "$(sha256sum "$P2POOL" | cut -d' ' -f1)"
}
EOF
chmod 644 "${D}/opt/kind-miner/engines/engines.json"

for unit in kind-minerd.service kind-miner.slice kind-minerd-upgrade.service kind-minerd-upgrade.timer; do
	install -Dm644 "${PKG}/systemd/${unit}" "${D}/usr/lib/systemd/system/${unit}"
done
install -Dm644 "${PKG}/deb/kind-miner.list" "${D}/etc/apt/sources.list.d/kind-miner.list"
install -Dm644 "${PKG}/deb/kind-miner.pref" "${D}/etc/apt/preferences.d/kind-miner"
install -Dm644 "$KEY" "${D}/usr/share/keyrings/kind-miner.asc"
install -Dm644 "${PKG}/deb/copyright" "${D}/usr/share/doc/kind-minerd/copyright"

install -d "${D}/DEBIAN"
for f in preinst postinst prerm postrm; do
	install -m755 "${PKG}/deb/${f}" "${D}/DEBIAN/${f}"
done
install -m644 "${PKG}/deb/conffiles" "${D}/DEBIAN/conffiles"
# Installed-Size from the files' own sizes, in KiB, rounded up per file the way
# dpkg-gencontrol does. Not `du`: it counts directory entries too, and their
# size depends on the filesystem (ext4 on the release runner, btrfs or tmpfs on
# a rebuilder), which made the control file — and so the whole .deb — differ
# between machines that built every file inside it identically.
SIZE="$(find "$D" -path "$D/DEBIAN" -prune -o -type f -printf '%s\n' |
	awk '{ kib += int(($1 + 1023) / 1024) } END { print kib + 0 }')"
sed -e "s/@VERSION@/${DEB_VERSION}/" -e "s/@ARCH@/${ARCH}/" -e "s/@SIZE@/${SIZE}/" \
	"${PKG}/deb/control.in" > "${D}/DEBIAN/control"
(cd "$D" && find . -path ./DEBIAN -prune -o -type f -print | LC_ALL=C sort | sed 's|^\./||' |
	xargs md5sum > DEBIAN/md5sums)
chmod 644 "${D}/DEBIAN/control" "${D}/DEBIAN/md5sums"

# Everything a build could leak into the archive, pinned: the modes above,
# mtimes clamped to the commit, owners root (--root-owner-group), and the
# order dpkg-deb sorts itself.
find "$D" -type d -exec chmod 755 {} +
find "$D" -exec touch -h -d "@${SOURCE_DATE_EPOCH}" {} +

mkdir -p "$OUT"
DEB="${OUT}/kind-minerd_${VERSION}_${ARCH}.deb"
dpkg-deb --root-owner-group -Zxz --build "$D" "$DEB" >/dev/null
echo "$DEB"
