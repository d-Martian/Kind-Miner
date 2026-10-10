#!/usr/bin/env bash
# Makes a flat, signed apt repository from .deb files:
#
#   scripts/build-apt-repo.sh DEB_DIR OUT_DIR
#
# OUT_DIR gets the .debs, Packages(.gz), Release, and Release signed both
# ways apt reads (InRelease, Release.gpg), with the one secret key in the
# current GNUPGHOME. Flat — `deb … ./` — because two packages, kind-minerd
# and kind-miner, in at most two architectures need no pool or dists tree.
#
# Release dates come from SOURCE_DATE_EPOCH, so the same .debs make the same
# unsigned files; the signatures differ each time, as signatures do.
set -euo pipefail

[[ $# -eq 2 ]] || { sed -n '2,12p' "$0" >&2; exit 2; }
DEBS="$1" OUT="$2"
: "${SOURCE_DATE_EPOCH:?set SOURCE_DATE_EPOCH}"

mkdir -p "$OUT"
cp "$DEBS"/*.deb "$OUT/"
cd "$OUT"
apt-ftparchive packages . > Packages
gzip -9n < Packages > Packages.gz
DATE="$(LC_ALL=C date -u -d "@${SOURCE_DATE_EPOCH}" '+%a, %d %b %Y %H:%M:%S UTC')"
apt-ftparchive \
	-o APT::FTPArchive::Release::Origin=kind-miner \
	-o APT::FTPArchive::Release::Label=kind-miner \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Architectures="amd64 arm64" \
	-o APT::FTPArchive::Release::Date="$DATE" \
	release . > Release

KEYS="$(gpg --batch --with-colons --list-secret-keys | grep -c '^sec' || true)"
[[ "$KEYS" = 1 ]] || { echo "expected exactly one secret key in GNUPGHOME, found $KEYS" >&2; exit 1; }
gpg --batch --yes --clearsign --digest-algo SHA512 -o InRelease Release
gpg --batch --yes --armor --detach-sign --digest-algo SHA512 -o Release.gpg Release
