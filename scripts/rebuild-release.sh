#!/usr/bin/env bash
# A second rebuilder: rebuilds a published release on this machine, from the
# tag, with the same recipes the release workflow ran, and checks that every
# file it can rebuild is byte for byte what the release published.
#
#   scripts/rebuild-release.sh TAG [--sign] [--upload] [--against DIR]
#
# What it rebuilds — everything an x86_64 Linux machine with podman can:
#
#   kind-miner-TAG-linux-amd64.tar.gz       scripts/release-gui-linux.sh
#   kind-minerd_X_amd64.deb                 scripts/release-deb.sh, with
#                                           xmrig from scripts/build-xmrig.sh
#   kind-miner-TAG-xmrig-*-source.tar.gz    scripts/source-tarballs.sh
#   p2pool_source-v*.tar.xz                 (p2pool's own, checked by pin)
#
# It never rebuilds what no container can: the macOS and Windows builds, the
# AppImage and the Flatpak, or the arm64 .deb (which needs an arm64 machine).
#
# It writes SHA256SUMS.rebuilt-HOST: the lines it reproduced, and only those.
# --sign signs that file with this rebuilder's minisign key
# (~/.minisign/kind-miner-rebuilder.key, made on first use; its public half
# goes in rebuilders/HOST.pub, to be committed), which is the co-signature: a
# second machine, not GitHub's, vouching for those bytes. --upload attaches
# both to the release, and only when asked. --against DIR compares with the
# files in DIR instead of downloading the release's.
#
# Exit status 1 if anything it rebuilt differs.
set -euo pipefail

TAG="" SIGN=0 UPLOAD=0 AGAINST=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--sign) SIGN=1 ;;
	--upload) UPLOAD=1 SIGN=1 ;;
	--against) AGAINST="$(cd "$2" && pwd)"; shift ;;
	-*) sed -n '2,27p' "$0" >&2; exit 2 ;;
	*) TAG="$1" ;;
	esac
	shift
done
[[ -n "$TAG" ]] || { sed -n '2,27p' "$0" >&2; exit 2; }
[[ "$(uname -m)" == x86_64 ]] || { echo "this rebuilder covers the x86_64 artifacts; run it on x86_64" >&2; exit 1; }
for tool in podman git; do command -v "$tool" >/dev/null || { echo "needs $tool" >&2; exit 1; }; done
[[ -n "$AGAINST" ]] || command -v gh >/dev/null || { echo "needs gh to download the release (or --against DIR)" >&2; exit 1; }
[[ $SIGN == 0 ]] || command -v minisign >/dev/null || { echo "--sign needs minisign" >&2; exit 1; }

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="$(hostname -s | tr -c 'A-Za-z0-9-\n' -)"
WORK="$(mktemp -d)"
trap 'git -C "$ROOT" worktree remove --force "$WORK/src" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT

# The tag's own tree, not this checkout: the recipes as they were then.
git -C "$ROOT" fetch -q --tags origin 2>/dev/null || true
git -C "$ROOT" worktree add -q --detach "$WORK/src" "$TAG"
cd "$WORK/src"
export SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
OUT="$WORK/rebuilt"
mkdir -p "$OUT"

echo "rebuilding ${TAG} (SOURCE_DATE_EPOCH ${SOURCE_DATE_EPOCH}) on ${HOST}" >&2
scripts/release-gui-linux.sh "$TAG" "$OUT" >/dev/null
scripts/build-xmrig.sh "$WORK/xmrig" >/dev/null
bash scripts/download-p2pool.sh >/dev/null
scripts/release-deb.sh "$TAG" "$WORK/xmrig/xmrig" dist/linux-amd64/bin/p2pool "$OUT" >/dev/null
scripts/source-tarballs.sh "$TAG" "$OUT" >/dev/null
rm -f "$OUT"/*.sha256 "$OUT/SHA256SUMS-source"

# What the release published, for the same names.
PUB="$WORK/published"
mkdir -p "$PUB"
if [[ -n "$AGAINST" ]]; then
	for f in "$OUT"/*; do cp "$AGAINST/$(basename "$f")" "$PUB/" 2>/dev/null || true; done
else
	for f in "$OUT"/*; do
		gh release download "$TAG" -R d-Martian/Kind-Miner -p "$(basename "$f")" -D "$PUB" 2>/dev/null || true
	done
fi

SUMS="$ROOT/SHA256SUMS.rebuilt-${HOST}"
: > "$SUMS"
bad=0
printf '\n%-52s %s\n' "file" "result" >&2
for f in "$OUT"/*; do
	name="$(basename "$f")"
	mine="$(sha256sum "$f" | cut -d' ' -f1)"
	if [[ ! -f "$PUB/$name" ]]; then
		printf '%-52s %s\n' "$name" "not in the release" >&2
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
LC_ALL=C sort -k2 -o "$SUMS" "$SUMS"
echo >&2
echo "$SUMS" >&2

if [[ $SIGN == 1 ]]; then
	KEY="${KM_REBUILDER_KEY:-$HOME/.minisign/kind-miner-rebuilder.key}"
	PUBKEY="$ROOT/rebuilders/${HOST}.pub"
	if [[ ! -f "$KEY" ]]; then
		echo "making this rebuilder's key; commit ${PUBKEY} afterwards" >&2
		mkdir -p "$(dirname "$KEY")" "$ROOT/rebuilders"
		minisign -G -p "$PUBKEY" -s "$KEY"
	fi
	minisign -S -l -s "$KEY" -m "$SUMS" -x "$SUMS.minisig" \
		-t "kind-miner ${TAG}: $(wc -l < "$SUMS") files reproduced by ${HOST}"
	if [[ $UPLOAD == 1 && $bad == 0 ]]; then
		gh release upload "$TAG" -R d-Martian/Kind-Miner "$SUMS" "$SUMS.minisig" --clobber
	elif [[ $UPLOAD == 1 ]]; then
		echo "not uploading: something differed" >&2
	fi
fi
exit "$bad"
