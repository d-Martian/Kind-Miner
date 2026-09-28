#!/usr/bin/env bash
# Makes the apt repository's signing key, once. The private half goes
# straight into the GitHub Actions secret APT_SIGNING_KEY, where the release
# workflow signs with it, and is then deleted: it never touches the working
# tree, and nobody needs to hold a copy. The public half is written to
# packaging/deb/kind-miner.asc, to be committed: every .deb carries it, and it
# is what apt checks the repository against.
#
# Losing the secret means a new key and a release carrying it, installed by
# hand once on each Nodo — so this refuses to replace an existing key.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PUB="${ROOT}/packaging/deb/kind-miner.asc"
[[ ! -e "$PUB" ]] || { echo "$PUB already exists; rotating the key is a deliberate release, not this script" >&2; exit 1; }
command -v gh >/dev/null || { echo "needs the gh CLI, logged in to the repository" >&2; exit 1; }

export GNUPGHOME="$(mktemp -d)"
trap 'gpgconf --kill all 2>/dev/null; rm -rf "$GNUPGHOME"' EXIT

gpg --batch --quiet --passphrase '' --quick-gen-key \
	'kind-miner apt repository <d-Martian@users.noreply.github.com>' ed25519 sign never
FPR="$(gpg --batch --with-colons --list-secret-keys | awk -F: '$1 == "fpr" { print $10; exit }')"

gpg --batch --armor --export-secret-keys "$FPR" | gh secret set APT_SIGNING_KEY
gpg --batch --armor --export "$FPR" > "$PUB"
echo "Stored the private key as the APT_SIGNING_KEY secret; wrote $PUB."
echo "Fingerprint: $FPR"
echo "Commit $PUB."
