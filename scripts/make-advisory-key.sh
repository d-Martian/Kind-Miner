#!/usr/bin/env bash
# Makes the advisory's signing key, once, with minisign. The secret key stays
# on this machine, encrypted with the password minisign asks for — keep a
# backup somewhere offline; losing it means a release carrying a new public
# key before any advisory can be signed again. The public key is written to
# internal/advisory/minisign.pub, to be committed: every build embeds it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PUB="${ROOT}/internal/advisory/minisign.pub"
KEY="${KM_ADVISORY_KEY:-$HOME/.minisign/kind-miner-advisory.key}"
command -v minisign >/dev/null || { echo "needs minisign (dnf/apt install minisign)" >&2; exit 1; }
[[ ! -s "$PUB" ]] || { echo "$PUB already holds a key; replacing it is a deliberate release, not this script" >&2; exit 1; }
[[ ! -e "$KEY" ]] || { echo "$KEY already exists" >&2; exit 1; }

mkdir -p "$(dirname "$KEY")"
minisign -G -p "$PUB" -s "$KEY"
echo "Secret key: $KEY (back it up offline)."
echo "Commit $PUB."
