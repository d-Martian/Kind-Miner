#!/usr/bin/env bash
# Builds the kind-miner desktop .deb, reproducibly: the same inputs and
# SOURCE_DATE_EPOCH give the same file, byte for byte.
#
#   scripts/build-deb-desktop.sh VERSION ARCH KIND_MINER ENGINES OUT_DIR
#
#   VERSION     v1.2.3 or 1.2.3
#   ARCH        amd64 (the GUI release is built for amd64 only)
#   KIND_MINER  the GUI, built with the release flags (scripts/reproduce.sh)
#   ENGINES     the directory tools/stage-engines made: xmrig built from
#               source, the pinned p2pool and Tor, engines.json
#
# This is the app the AppImage and the Flatpak carry, installed system-wide
# and upgraded by apt. It conflicts with kind-minerd, the service for
# machines with no screen, so one machine never runs two miners. Prints the
# .deb's path.
#
# Layout:
#   /opt/kind-miner-desktop/kind-miner
#   /opt/kind-miner-desktop/engines/{xmrig,p2pool,tor/,engines.json}
#   /usr/bin/kind-miner -> /opt/kind-miner-desktop/kind-miner
#   /usr/share/applications/kind-miner.desktop
#   /usr/share/icons/hicolor/{256x256,512x512}/apps/kind-miner.png
#   /usr/lib/systemd/system/kind-miner-upgrade.{service,timer}
#   /etc/apt/sources.list.d/kind-miner.list, /etc/apt/preferences.d/kind-miner
#   /usr/share/keyrings/kind-miner.asc
# Each user's data stays in their own home (~/.local/share/kind-miner), since
# /opt is not theirs to write.
set -euo pipefail

[[ $# -eq 5 ]] || { sed -n '2,16p' "$0" >&2; exit 2; }
VERSION="${1#v}" ARCH="$2" KM="$3" ENGINES="$4" OUT="$5"
: "${SOURCE_DATE_EPOCH:?set SOURCE_DATE_EPOCH (make passes the commit time)}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="${ROOT}/packaging"
KEY="${KM_APT_KEY:-${PKG}/deb/kind-miner.asc}" # the override is for tests
[[ -f "$KEY" ]] || { echo "no repository key at $KEY: run scripts/make-apt-key.sh first" >&2; exit 1; }
[[ "$ARCH" == amd64 ]] || { echo "ARCH must be amd64" >&2; exit 2; }
[[ -f "${ENGINES}/engines.json" ]] || { echo "no staged engines in ${ENGINES}: run go run ./tools/stage-engines first" >&2; exit 1; }
[[ "$VERSION" =~ ^[0-9][0-9A-Za-z.+~-]*$ ]] || { echo "not a Debian version: $VERSION" >&2; exit 2; }
# 0.2.0-rc1 becomes 0.2.0~rc1, for the reason build-deb.sh gives.
DEB_VERSION="${VERSION/-/\~}"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
D="${STAGE}/kind-miner"
APP="${D}/opt/kind-miner-desktop"

install -Dm755 "$KM" "${APP}/kind-miner"
install -d "${D}/usr/bin"
ln -s /opt/kind-miner-desktop/kind-miner "${D}/usr/bin/kind-miner"
mkdir -p "${APP}"
cp -R "$ENGINES" "${APP}/engines"
# The staged files' modes come from whatever unpacked them; pin them to two,
# so a rebuilder with a different umask packs the same bytes.
find "${APP}/engines" -type f -perm /111 -exec chmod 755 {} +
find "${APP}/engines" -type f ! -perm /111 -exec chmod 644 {} +

install -Dm644 "${ROOT}/kind-miner.desktop" "${D}/usr/share/applications/kind-miner.desktop"
install -Dm644 "${ROOT}/assets/icons/app-256.png" "${D}/usr/share/icons/hicolor/256x256/apps/kind-miner.png"
install -Dm644 "${ROOT}/assets/icons/app-512.png" "${D}/usr/share/icons/hicolor/512x512/apps/kind-miner.png"
for unit in kind-miner-upgrade.service kind-miner-upgrade.timer; do
	install -Dm644 "${PKG}/systemd/${unit}" "${D}/usr/lib/systemd/system/${unit}"
done
install -Dm644 "${PKG}/deb/kind-miner.list" "${D}/etc/apt/sources.list.d/kind-miner.list"
install -Dm644 "${PKG}/deb/kind-miner.pref" "${D}/etc/apt/preferences.d/kind-miner"
install -Dm644 "$KEY" "${D}/usr/share/keyrings/kind-miner.asc"
install -Dm644 "${PKG}/deb-desktop/copyright" "${D}/usr/share/doc/kind-miner/copyright"

install -d "${D}/DEBIAN"
for f in postinst prerm postrm; do
	install -m755 "${PKG}/deb-desktop/${f}" "${D}/DEBIAN/${f}"
done
install -m644 "${PKG}/deb-desktop/conffiles" "${D}/DEBIAN/conffiles"
# Installed-Size from the files' own sizes, as build-deb.sh explains.
SIZE="$(find "$D" -path "$D/DEBIAN" -prune -o -type f -printf '%s\n' |
	awk '{ kib += int(($1 + 1023) / 1024) } END { print kib + 0 }')"
sed -e "s/@VERSION@/${DEB_VERSION}/" -e "s/@ARCH@/${ARCH}/" -e "s/@SIZE@/${SIZE}/" \
	"${PKG}/deb-desktop/control.in" > "${D}/DEBIAN/control"
(cd "$D" && find . -path ./DEBIAN -prune -o -type f -print | LC_ALL=C sort | sed 's|^\./||' |
	xargs md5sum > DEBIAN/md5sums)
chmod 644 "${D}/DEBIAN/control" "${D}/DEBIAN/md5sums"

find "$D" -type d -exec chmod 755 {} +
find "$D" -exec touch -h -d "@${SOURCE_DATE_EPOCH}" {} +

mkdir -p "$OUT"
DEB="${OUT}/kind-miner_${VERSION}_${ARCH}.deb"
dpkg-deb --root-owner-group -Zxz --build "$D" "$DEB" >/dev/null
echo "$DEB"
