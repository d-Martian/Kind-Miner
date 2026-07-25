#!/usr/bin/env bash
# Deterministic release archives.
#
# This is the single source of truth for the archive recipe: the Makefile
# bundle-* targets and .github/workflows/release.yml both call it, so the
# reproducibility flags cannot drift between a local build and CI.
#
# Without these flags a tar/zip stream embeds build-time mtimes, the packaging
# user's uid/gid, and directory-order-dependent member ordering, so the archive
# differs between builds even when the files inside it are identical.
# See REPRODUCIBLE.md and https://reproducible-builds.org.
#
# usage:
#   package.sh tar    <out.tar.gz> <chdir> <member>...
#   package.sh zip    <out.zip>    <chdir> <member>
#   package.sh sha256 <file>                 # writes <file>.sha256
set -euo pipefail

# Commit time — a deterministic clock. Same default as the Makefile.
: "${SOURCE_DATE_EPOCH:=$(git log -1 --format=%ct 2>/dev/null || echo 0)}"

# The flags below are GNU-specific; macOS ships bsdtar as `tar`, which silently
# lacks --sort and would produce a non-reproducible archive.
gnu_tar() {
	local t
	for t in gtar tar; do
		if command -v "$t" >/dev/null 2>&1 && "$t" --version 2>/dev/null | head -1 | grep -q GNU; then
			echo "$t"
			return 0
		fi
	done
	echo "package.sh: GNU tar not found (macOS: brew install gnu-tar)" >&2
	return 1
}

cmd_tar() {
	local out=$1 chdir=$2
	shift 2
	local tar
	tar=$(gnu_tar)
	# LC_ALL=C             locale-independent sort order
	# --sort=name          stable member order (not readdir order)
	# --format=posix       pin the archive format instead of inheriting tar's default
	# --mtime=@EPOCH       fixed timestamps
	# --owner/--group/--numeric-owner  drop the packaging user's identity
	# --pax-option         drop volatile atime/ctime pax headers
	# gzip -n              no filename/mtime in the gzip header
	LC_ALL=C "$tar" --sort=name --format=posix --mtime="@$SOURCE_DATE_EPOCH" \
		--owner=0 --group=0 --numeric-owner \
		--pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime \
		-cf - -C "$chdir" "$@" | gzip -9 -n > "$out"
}

cmd_zip() {
	local out=$1 chdir=$2 member=$3
	# zip has no --sort/--mtime: pin every member's mtime first, then feed a
	# sorted file list. -X drops extra attributes, -D drops directory entries.
	out=$(cd "$(dirname "$out")" && printf '%s/%s' "$(pwd)" "$(basename "$out")")
	(
		cd "$chdir"
		find "$member" -type f -exec touch -d "@$SOURCE_DATE_EPOCH" {} +
		find "$member" -type f | LC_ALL=C sort | zip -X -D -q "$out" -@
	)
}

# sha256sum is GNU coreutils; macOS ships shasum instead.
cmd_sha256() {
	local f=$1
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$f" > "$f.sha256"
	else
		shasum -a 256 "$f" > "$f.sha256"
	fi
}

case ${1:-} in
	tar)    shift; cmd_tar "$@" ;;
	zip)    shift; cmd_zip "$@" ;;
	sha256) shift; cmd_sha256 "$@" ;;
	*) echo "usage: package.sh {tar|zip|sha256} ..." >&2; exit 2 ;;
esac
