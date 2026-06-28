#!/bin/sh
set -eu

root="${1:-.}"
version="${2:-}"

printf 'Codeberg release readiness for %s\n' "$root"
printf '\n'

if ! git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'Not a git repository: %s\n' "$root"
  exit 1
fi

printf 'Git remotes:\n'
git -C "$root" remote -v || true

printf '\nWorking tree changes:\n'
changes="$(git -C "$root" status --short || true)"
if [ -n "$changes" ]; then
  printf '%s\n' "$changes"
else
  printf '  clean\n'
fi

printf '\nRecent tags:\n'
tags="$(git -C "$root" tag --sort=-v:refname | sed -n '1,10p' || true)"
if [ -n "$tags" ]; then
  printf '%s\n' "$tags" | sed 's/^/  /'
else
  printf '  none\n'
fi

if [ -n "$version" ]; then
  printf '\nRequested version:\n'
  printf '  %s\n' "$version"
  if printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'; then
    printf '  version shape: ok\n'
  else
    printf '  version shape: unusual; confirm project convention\n'
  fi

  if git -C "$root" rev-parse "$version" >/dev/null 2>&1; then
    tag_commit="$(git -C "$root" rev-list -n 1 "$version")"
    head_commit="$(git -C "$root" rev-parse HEAD)"
    printf '  tag exists: yes\n'
    printf '  tag commit: %s\n' "$tag_commit"
    printf '  HEAD commit: %s\n' "$head_commit"
    if [ "$tag_commit" = "$head_commit" ]; then
      printf '  HEAD matches tag: yes\n'
    else
      printf '  HEAD matches tag: no\n'
    fi
  else
    printf '  tag exists: no\n'
  fi
fi

printf '\nProject release hints:\n'
for path in Makefile go.mod package.json pyproject.toml Cargo.toml REPRODUCIBLE.md RELEASING.md CHANGELOG.md; do
  if [ -f "$root/$path" ]; then
    printf '  %s\n' "$path"
  fi
done

printf '\nTooling:\n'
for tool in git gpg curl jq tar go make sha256sum shasum; do
  if command -v "$tool" >/dev/null 2>&1; then
    printf '  %-10s %s\n' "$tool" "$(command -v "$tool")"
  else
    printf '  %-10s missing\n' "$tool"
  fi
done

printf '\nGPG secret keys:\n'
if command -v gpg >/dev/null 2>&1; then
  if gpg --list-secret-keys --keyid-format LONG >/tmp/codeberg-release-gpg-keys.$$ 2>/dev/null; then
    if [ -s /tmp/codeberg-release-gpg-keys.$$ ]; then
      sed -n '1,40p' /tmp/codeberg-release-gpg-keys.$$ | sed 's/^/  /'
    else
      printf '  none found\n'
    fi
    rm -f /tmp/codeberg-release-gpg-keys.$$
  else
    rm -f /tmp/codeberg-release-gpg-keys.$$
    printf '  unable to list keys\n'
  fi
else
  printf '  gpg missing\n'
fi

printf '\nManual Codeberg checks:\n'
printf '  - Releases enabled in repository settings\n'
printf '  - Scoped token with write:repository if publishing by API\n'
printf '  - Public GPG key added and verified on Codeberg if users should trust it there\n'
printf '  - Draft release reviewed before publication\n'

