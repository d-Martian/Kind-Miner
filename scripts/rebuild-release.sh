#!/usr/bin/env bash
# A second rebuilder: rebuilds a published release on this machine, from the
# tag, with the same recipes the release workflow ran, and checks that every
# file it can rebuild is byte for byte what the release published.
#
#   scripts/rebuild-release.sh TAG [--as NAME] [--sign] [--upload] [--against DIR]
#   scripts/rebuild-release.sh TAG --sign-file FILE [--as NAME] [--upload]
#
# What it rebuilds depends on the machine. On x86_64:
#
#   kind-miner-TAG-linux-amd64.tar.gz       scripts/release-gui-linux.sh
#   kind-minerd_X_amd64.deb                 scripts/release-deb.sh, with
#                                           xmrig from scripts/build-xmrig.sh
#   kind-miner_X_amd64.deb                  scripts/release-deb-desktop.sh, with
#                                           the engines from tools/stage-engines
#                                           (needs go; tags before it skip it)
#   kind-miner-TAG-xmrig-*-source.tar.gz    scripts/source-tarballs.sh
#   p2pool_source-v*.tar.xz                 (p2pool's own, checked by pin)
#
# On aarch64 (a Nodo, say), the one file only an arm64 machine can make:
#
#   kind-minerd_X_arm64.deb                 scripts/release-deb.sh
#
# It never rebuilds what no container can: the macOS and Windows builds, the
# AppImage and the Flatpak.
#
# It writes SHA256SUMS.rebuilt-NAME-ARCH: the lines it reproduced, and only
# those. NAME is who vouches for them — --as, or this repository's git
# user.name — never the machine, which is not an identity. --sign signs the
# file with NAME's own PGP key, one of those in rebuilders/NAME.asc (for the
# maintainer, a link to dMartian.pub) whose secret half is in this machine's
# gpg: the co-signature, someone with a machine that is not GitHub's
# vouching for those bytes. --upload attaches both to the release. Both
# look for that key before rebuilding anything, and stop if it cannot sign.
#
# The key need not be on the machine that rebuilds. Rebuild without --sign
# on the Nodo, copy its SHA256SUMS.rebuilt-NAME-arm64 to the machine that has
# the key, and there run --sign-file with --upload, which signs and uploads
# that file without rebuilding anything.
#
# --against DIR compares with the files in DIR instead of downloading the
# release's. Exit status 1 if anything it rebuilt differs.
set -euo pipefail

# The comment block above, however long it grows.
usage() { awk 'NR > 1 && /^#/ { print; next } NR > 1 { exit }' "$0" >&2; exit 2; }
TAG="" SIGN=0 UPLOAD=0 AGAINST="" WHO="" SIGN_FILE=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--sign) SIGN=1 ;;
	--upload) UPLOAD=1 SIGN=1 ;;
	--as) WHO="$2"; shift ;;
	--sign-file) SIGN_FILE="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"; SIGN=1; shift ;;
	--against) AGAINST="$(cd "$2" && pwd)"; shift ;;
	-*) usage ;;
	*) TAG="$1" ;;
	esac
	shift
done
[[ -n "$TAG" ]] || usage

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO=d-Martian/Kind-Miner
# The name only labels the list until it is signed, so an ordinary git name
# ("Jane Doe") or none at all must not stop a run that just compares
# bytes: it is made safe for a file name, as the hostname used to be.
# Signing is where it has to be exact — rebuilders/NAME.asc must exist.
WHO="${WHO:-$(git -C "$ROOT" config user.name || true)}"
WHO="$(printf '%s' "$WHO" | tr -c 'A-Za-z0-9._-' '-' | sed 's/^[-.]*//')"
WHO="${WHO:-unnamed}"
for a in amd64 arm64; do
	[[ "$WHO" != *-"$a" ]] || { echo "--as must not end in -${a}: that is where the architecture goes" >&2; exit 2; }
done
[[ $SIGN == 0 ]] || command -v gpg >/dev/null || { echo "--sign needs gpg" >&2; exit 1; }
[[ $UPLOAD == 0 ]] || command -v gh >/dev/null || { echo "--upload needs gh" >&2; exit 1; }

# signing_key: set FPR to the first of NAME's keys this machine can sign
# with, or say why there is none and stop. It runs before the rebuild, not
# after: a rebuild takes twenty minutes, and a key that was never going to
# sign should not cost them.
signing_key() {
	local keys="$ROOT/rebuilders/${WHO}.asc" fprs f err
	[[ -f "$keys" ]] || { echo "no key for ${WHO}: add their PGP public key as ${keys}" >&2; exit 1; }
	# Ask gpg whether it can read its keyring at all before trusting its answer
	# about any one key. A gpg that cannot open the keyring fails every lookup
	# just as a missing key does. Signing rc9 hit exactly that: a lock left by a
	# gpg that died, whose PID a reboot gave to a kernel thread, so gpg waited
	# on it and gave up. The script said "no secret half" and sent the hunt to
	# a key that was there all along.
	if ! err="$(gpg --batch --list-secret-keys 2>&1 >/dev/null)"; then
		echo "gpg cannot read this machine's keyring, so this cannot sign. gpg said:" >&2
		printf '%s\n' "$err" | sed 's/^/  /' >&2
		exit 1
	fi
	fprs="$(gpg --batch --with-colons --import-options show-only --import "$keys" 2>/dev/null |
		awk -F: '$1 == "pub" { want = 1 } $1 == "fpr" && want { print $10; want = 0 }')" || true
	[[ -n "$fprs" ]] || { echo "gpg found no public key in ${keys}" >&2; exit 1; }
	for f in $fprs; do
		if gpg --batch --list-secret-keys "$f" >/dev/null 2>&1; then FPR="$f"; return; fi
	done
	echo "none of the keys in ${keys} has its secret half in this machine's gpg" >&2
	exit 1
}

# sign FILE: the co-signature, with the key signing_key found, then, if
# asked, the upload.
sign() {
	local sums="$1"
	gpg --batch --yes --local-user "$FPR" --armor --detach-sign --output "$sums.asc" "$sums"
	echo "signed $(basename "$sums") as ${WHO} (${FPR})" >&2
	if [[ $UPLOAD == 1 ]]; then
		gh release upload "$TAG" -R "$REPO" "$sums" "$sums.asc" --clobber
	fi
}

if [[ -n "$SIGN_FILE" ]]; then
	case "$(basename "$SIGN_FILE")" in
	SHA256SUMS.rebuilt-"$WHO"-amd64 | SHA256SUMS.rebuilt-"$WHO"-arm64) ;;
	*) echo "$SIGN_FILE is not a SHA256SUMS.rebuilt-${WHO}-ARCH file (pass --as to sign as someone else)" >&2; exit 2 ;;
	esac
	# A signature on an empty list vouches for nothing, yet looks like a
	# co-signature on the release page.
	[[ -s "$SIGN_FILE" ]] || { echo "$SIGN_FILE is empty: it reproduced nothing, so there is nothing to sign" >&2; exit 1; }
fi
FPR=""
[[ $SIGN == 0 ]] || signing_key
if [[ -n "$SIGN_FILE" ]]; then
	sign "$SIGN_FILE"
	exit 0
fi

case "$(uname -m)" in
	x86_64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) echo "this rebuilder covers x86_64 and aarch64; this is $(uname -m)" >&2; exit 1 ;;
esac
for tool in podman git; do command -v "$tool" >/dev/null || { echo "needs $tool" >&2; exit 1; }; done

# Make sure the release can be seen from here before spending twenty minutes
# rebuilding it. A draft is visible only to a logged-in maintainer: rc10's
# arm64 rebuild ran on a Nodo with plain curl while the release was still a
# draft, found "not in the release" for a file that was there, and left an
# empty list behind. gh is used only when it can actually see the release
# (installed but not logged in, it cannot download at all); otherwise curl,
# which sees what anyone sees.
FETCH=""
if [[ -z "$AGAINST" ]]; then
	if command -v gh >/dev/null && gh release view "$TAG" -R "$REPO" >/dev/null 2>&1; then
		FETCH=gh
	elif command -v curl >/dev/null; then
		curl -fsS -o /dev/null "https://api.github.com/repos/${REPO}/releases/tags/${TAG}" 2>/dev/null || {
			echo "release ${TAG} cannot be seen from this machine: either it does not exist yet, or it is" >&2
			echo "still a draft, which only a logged-in maintainer can see. Publish it, run this where gh" >&2
			echo "is logged in, or pass --against DIR with the release's files copied into DIR." >&2
			exit 1
		}
		FETCH=curl
	else
		echo "needs gh or curl to download the release (or --against DIR)" >&2
		exit 1
	fi
fi

WORK="$(mktemp -d)"
trap 'git -C "$ROOT" worktree remove --force "$WORK/src" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT

# The tag's own tree, not this checkout: the recipes as they were then.
git -C "$ROOT" fetch -q --tags origin 2>/dev/null || true
git -C "$ROOT" worktree add -q --detach "$WORK/src" "$TAG"
cd "$WORK/src"
export SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
OUT="$WORK/rebuilt"
mkdir -p "$OUT"

echo "rebuilding ${TAG} (SOURCE_DATE_EPOCH ${SOURCE_DATE_EPOCH}) for ${ARCH}, vouched for by ${WHO}" >&2
scripts/build-xmrig.sh "$WORK/xmrig" >/dev/null
bash scripts/download-p2pool.sh >/dev/null
scripts/release-deb.sh "$TAG" "$WORK/xmrig/xmrig" "dist/linux-${ARCH}/bin/p2pool" "$OUT" >/dev/null
if [[ $ARCH == amd64 ]]; then
	scripts/release-gui-linux.sh "$TAG" "$OUT" >/dev/null
	if [[ -x scripts/release-deb-desktop.sh ]]; then
		command -v go >/dev/null || { echo "needs go, to stage the desktop .deb's engines" >&2; exit 1; }
		go run ./tools/stage-engines -xmrig "$WORK/xmrig/xmrig" -out "$WORK/engines" >/dev/null
		scripts/release-deb-desktop.sh "$TAG" "$WORK/engines" "$OUT" >/dev/null
	fi
	scripts/source-tarballs.sh "$TAG" "$OUT" >/dev/null
fi
rm -f "$OUT"/*.sha256 "$OUT/SHA256SUMS-source"

# What the release published, for the same names.
PUB="$WORK/published"
mkdir -p "$PUB"
for f in "$OUT"/*; do
	name="$(basename "$f")"
	if [[ -n "$AGAINST" ]]; then
		cp "$AGAINST/$name" "$PUB/" 2>/dev/null || true
	elif [[ $FETCH == gh ]]; then
		gh release download "$TAG" -R "$REPO" -p "$name" -D "$PUB" 2>/dev/null || true
	else
		curl -fsSL -o "$PUB/$name" "https://github.com/${REPO}/releases/download/${TAG}/${name}" 2>/dev/null || rm -f "$PUB/$name"
	fi
done

SUMS="$ROOT/SHA256SUMS.rebuilt-${WHO}-${ARCH}"
: > "$SUMS"
rm -f "$SUMS.asc" # signed an earlier list, never this one
bad=0
printf '\n%-52s %s\n' "file" "result" >&2
for f in "$OUT"/*; do
	name="$(basename "$f")"
	mine="$(sha256sum "$f" | cut -d' ' -f1)"
	if [[ ! -f "$PUB/$name" ]]; then
		printf '%-52s %s\n' "$name" "not in ${AGAINST:-the release}" >&2
		bad=1
		continue
	fi
	theirs="$(sha256sum "$PUB/$name" | cut -d' ' -f1)"
	if [[ "$mine" == "$theirs" ]]; then
		printf '%-52s %s\n' "$name" "reproduced" >&2
		echo "${mine}  ${name}" >> "$SUMS"
	else
		printf '%-52s %s\n' "$name" "DIFFERS (${mine:0:12} here, ${theirs:0:12} published)" >&2
		bad=1
	fi
done
echo >&2
if [[ ! -s "$SUMS" ]]; then
	# An empty list is easy to mistake for a finished one and copy off to be
	# signed, so leave none.
	rm -f "$SUMS"
	echo "nothing was reproduced, so no SHA256SUMS.rebuilt-${WHO}-${ARCH} was written" >&2
	exit 1
fi
LC_ALL=C sort -k2 -o "$SUMS" "$SUMS"
echo "$SUMS" >&2

if [[ $bad == 1 ]]; then
	[[ $SIGN == 0 ]] || echo "not signing: something differed" >&2
elif [[ $SIGN == 1 ]]; then
	sign "$SUMS"
else
	echo "to co-sign, where ${WHO}'s key is: scripts/rebuild-release.sh ${TAG} --as ${WHO} --sign-file $(basename "$SUMS") --upload" >&2
fi
exit "$bad"
