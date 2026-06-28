#!/bin/sh
# Build an artifact twice and compare SHA256. On mismatch, run diffoscope to
# show *where* the bytes differ. This proves local determinism (rung 1); set
# VARY=1 to perturb the environment on the second build for a cheap taste of
# cross-environment reproducibility (rung 2). For the full matrix use reprotest.
#
# Usage: sh rebuild_compare.sh "<build command>" <artifact> [<artifact> ...]
#   env: VARY=1        perturb TZ/locale/umask/HOME on the 2nd build
#        CLEAN='<cmd>'  run before each build (e.g. 'make clean')
set -eu

if [ $# -lt 2 ]; then
  echo "usage: rebuild_compare.sh \"<build command>\" <artifact> [<artifact> ...]" >&2
  echo "  env: VARY=1 perturb env on 2nd build; CLEAN='<cmd>' run before each build" >&2
  exit 2
fi

build_cmd=$1; shift
artifacts="$*"

if command -v sha256sum >/dev/null 2>&1; then SHA=sha256sum; else SHA="shasum -a 256"; fi

: "${SOURCE_DATE_EPOCH:=$(git log -1 --pretty=%ct 2>/dev/null || echo 0)}"
export SOURCE_DATE_EPOCH

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

safe_name() { printf '%s' "$1" | tr '/ ' '__'; }

hash_into() { # subdir -> prints "<sha>  <safe-name>" per artifact
  d="$tmp/$1"; mkdir -p "$d"
  for a in $artifacts; do
    if [ ! -f "$a" ]; then echo "artifact not found / not a file after build: $a" >&2; exit 1; fi
    cp "$a" "$d/$(safe_name "$a")"
  done
  ( cd "$d" && $SHA -- * )
}

run_build() { # label  env-assignments
  echo ">>> build $1 ${2:+[$2]}" >&2
  if [ -n "${CLEAN:-}" ]; then sh -c "$CLEAN" >&2 || true; fi
  # shellcheck disable=SC2086  # $2 is intentionally split into KEY=VAL args
  env $2 sh -c "$build_cmd" >&2
}

run_build A ""
a="$(hash_into a)"

vary=""
if [ "${VARY:-0}" = "1" ]; then
  mkdir -p "$tmp/home"
  vary="TZ=Pacific/Kiritimati LANG=C LC_ALL=C HOME=$tmp/home"
  umask 077
fi
run_build B "$vary"
b="$(hash_into b)"
umask 022

echo
echo "build A:"; printf '%s\n' "$a" | sed 's/^/  /'
echo "build B:"; printf '%s\n' "$b" | sed 's/^/  /'
echo

ha="$(printf '%s\n' "$a" | awk '{print $1}')"
hb="$(printf '%s\n' "$b" | awk '{print $1}')"
if [ "$ha" = "$hb" ]; then
  echo "✓ reproducible — identical bytes${vary:+ (across varied environment)}"
  exit 0
fi

echo "✗ NOT reproducible — builds differ"
if command -v diffoscope >/dev/null 2>&1; then
  for a in $artifacts; do
    n="$(safe_name "$a")"
    echo "--- diffoscope: $a ---"
    diffoscope "$tmp/a/$n" "$tmp/b/$n" || true
  done
else
  echo "(install diffoscope to see where they differ: pip install diffoscope)"
  echo " then map each difference to references/nondeterminism-catalog.md"
fi
exit 1
