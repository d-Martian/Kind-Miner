#!/bin/sh
set -eu

root="${1:-.}"

printf 'Codeberg CI/CD inventory for %s\n' "$root"
printf '\n'

if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'Git remotes:\n'
  git -C "$root" remote -v || true
  printf '\n'
  printf 'Working tree changes:\n'
  git -C "$root" status --short || true
else
  printf 'Not a git repository: %s\n' "$root"
fi

printf '\nCI files:\n'
found=0
for path in \
  "$root/.woodpecker.yml" \
  "$root/.woodpecker.yaml"
do
  if [ -f "$path" ]; then
    printf '  %s\n' "${path#"$root"/}"
    found=1
  fi
done

if [ -d "$root/.woodpecker" ]; then
  find "$root/.woodpecker" -maxdepth 2 -type f \( -name '*.yml' -o -name '*.yaml' \) -print | sed "s#^$root/#  #"
  found=1
fi

if [ -d "$root/.forgejo/workflows" ]; then
  find "$root/.forgejo/workflows" -maxdepth 1 -type f \( -name '*.yml' -o -name '*.yaml' \) -print | sed "s#^$root/#  #"
  found=1
fi

if [ -d "$root/.github/workflows" ]; then
  find "$root/.github/workflows" -maxdepth 1 -type f \( -name '*.yml' -o -name '*.yaml' \) -print | sed "s#^$root/#  #"
  found=1
fi

if [ "$found" -eq 0 ]; then
  printf '  none found\n'
fi

printf '\nProject hints:\n'
for path in Makefile go.mod package.json pyproject.toml Cargo.toml pom.xml build.gradle; do
  if [ -f "$root/$path" ]; then
    printf '  %s\n' "$path"
  fi
done

printf '\nRelease hints:\n'
if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  latest_tag="$(git -C "$root" describe --tags --abbrev=0 2>/dev/null || true)"
  if [ -n "$latest_tag" ]; then
    printf '  latest tag: %s\n' "$latest_tag"
  else
    printf '  no tags found\n'
  fi
fi

for path in RELEASING.md RELEASE.md CHANGELOG.md REPRODUCIBLE.md; do
  if [ -f "$root/$path" ]; then
    printf '  %s\n' "$path"
  fi
done

printf '\nManual Codeberg checks:\n'
printf '  - Woodpecker repository activation at https://ci.codeberg.org\n'
printf '  - Repository Actions enabled if using .forgejo/workflows\n'
printf '  - Required secrets present in the selected CI system\n'
printf '  - Releases and/or Pages enabled if workflows publish them\n'

