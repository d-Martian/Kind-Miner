#!/usr/bin/env bash
set -euo pipefail

run() {
  echo
  echo "+ $*"
  "$@"
}

missing=0
for tool in git bash go make; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool" >&2
    missing=1
  fi
done
if [[ "$missing" -ne 0 ]]; then
  exit 1
fi

run git diff --check
run go test ./...
run make verify-repro

if [[ "${ALLOW_NETWORK_DOWNLOADS:-0}" == "1" ]]; then
  run bash scripts/download-xmrig.sh
  run bash scripts/download-p2pool.sh
else
  echo
  echo "Skipping dependency archive downloads. Set ALLOW_NETWORK_DOWNLOADS=1 to run them."
fi

echo
echo "Dependency update validation completed."

