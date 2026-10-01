#!/usr/bin/env bash
# Signs advisory/manifest.json with the offline advisory key, after checking
# it the way every client will: a valid manifest, a lifetime of at most 60
# days, and a serial above the one already committed. Commit both files and
# push; the Pages workflow verifies the signature again and publishes them.
#
# Re-sign within 60 days even when nothing changes — with a new serial and
# new dates — or clients will report the advisory as expired. An expired one
# stops nothing; it only stops counting.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
M=advisory/manifest.json
KEY="${KM_ADVISORY_KEY:-$HOME/.minisign/kind-miner-advisory.key}"
command -v minisign >/dev/null || { echo "needs minisign (dnf/apt install minisign)" >&2; exit 1; }

prev="$(git show "HEAD:$M" 2>/dev/null | sed -n 's/.*"serial": *\([0-9]*\).*/\1/p' | head -1)"
if git ls-files --error-unmatch "$M.minisig" >/dev/null 2>&1; then
	go run ./tools/advisory check "$M" "${prev:-0}"
else
	go run ./tools/advisory check "$M" # nothing published yet
fi
serial="$(sed -n 's/.*"serial": *\([0-9]*\).*/\1/p' "$M" | head -1)"

# -l: Ed25519 over the whole file, which kind-miner verifies with the
# standard library alone; minisign's default would need BLAKE2b.
minisign -S -l -s "$KEY" -m "$M" -x "$M.minisig" -t "kind-miner advisory serial ${serial}"
go run ./tools/advisory verify "$M"
