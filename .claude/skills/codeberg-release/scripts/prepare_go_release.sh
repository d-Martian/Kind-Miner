#!/bin/sh
set -eu

usage() {
  cat <<'USAGE'
Usage: prepare_go_release.sh VERSION [OUTDIR]

Builds Go release archives twice per target, compares binary hashes, writes
SHA256SUMS, signs SHA256SUMS with GPG, and creates release notes.

Environment:
  BINARY_NAME              output binary name; default: repo directory name
  MAIN_PACKAGE             main package path; default: ./cmd/$BINARY_NAME or .
  TARGETS                  space-separated GOOS/GOARCH list
  CGO_ENABLED              default: 0
  GOTOOLCHAIN              default: auto
  GO_LDFLAGS               default: -s -w -buildid= -X main.version=$VERSION
  GPG_KEY                  key id or fingerprint for signing
  SIGN_ARTIFACTS=1         sign each archive as well as SHA256SUMS
  EXPORT_GPG_PUBLIC_KEY=1  export GPG-PUBLIC-KEY.asc
  ALLOW_DIRTY=1            allow a dirty working tree
  REQUIRE_TAG=0            do not require VERSION tag at HEAD
USAGE
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

version="${1:-}"
outdir="${2:-}"

if [ -z "$version" ]; then
  usage
  exit 2
fi

if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'; then
  printf 'Version does not look like vMAJOR.MINOR.PATCH: %s\n' "$version" >&2
  printf 'Set the project convention intentionally before continuing.\n' >&2
  exit 2
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'Run from inside a git repository.\n' >&2
  exit 1
fi

root="$(git rev-parse --show-toplevel)"
cd "$root"

if [ "${ALLOW_DIRTY:-0}" != "1" ] && [ -n "$(git status --porcelain)" ]; then
  printf 'Working tree is dirty. Commit/stash unrelated changes or set ALLOW_DIRTY=1.\n' >&2
  git status --short >&2
  exit 1
fi

head_commit="$(git rev-parse HEAD)"
tag_commit="$(git rev-list -n 1 "$version" 2>/dev/null || true)"

if [ "${REQUIRE_TAG:-1}" != "0" ]; then
  if [ -z "$tag_commit" ]; then
    printf 'Required release tag does not exist: %s\n' "$version" >&2
    printf 'Create it with: git tag -a %s -m "%s"\n' "$version" "$version" >&2
    exit 1
  fi
  if [ "$tag_commit" != "$head_commit" ]; then
    printf 'HEAD does not match tag %s.\n' "$version" >&2
    printf 'HEAD: %s\nTag:  %s\n' "$head_commit" "$tag_commit" >&2
    exit 1
  fi
fi

if ! command -v go >/dev/null 2>&1; then
  printf 'go is required.\n' >&2
  exit 1
fi

if ! command -v gpg >/dev/null 2>&1; then
  printf 'gpg is required.\n' >&2
  exit 1
fi

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1"
  else
    printf 'sha256sum or shasum is required.\n' >&2
    exit 1
  fi
}

binary_name="${BINARY_NAME:-$(basename "$root")}"
main_package="${MAIN_PACKAGE:-}"
if [ -z "$main_package" ]; then
  if [ -d "./cmd/$binary_name" ]; then
    main_package="./cmd/$binary_name"
  elif [ -f "./main.go" ]; then
    main_package="."
  else
    main_package="./cmd/$binary_name"
  fi
fi

targets="${TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64}"
cgo_enabled="${CGO_ENABLED:-0}"
go_toolchain="${GOTOOLCHAIN:-auto}"
go_ldflags="${GO_LDFLAGS:--s -w -buildid= -X main.version=$version}"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}"

if [ -z "$outdir" ]; then
  outdir="dist/release-$version"
fi

case "$outdir" in
  ""|"/"|"."|"$root")
    printf 'Refusing unsafe release output directory: %s\n' "$outdir" >&2
    exit 1
    ;;
esac

rm -rf "$outdir"
mkdir -p "$outdir"

tmp_a="$(mktemp -d "${TMPDIR:-/tmp}/codeberg-release-a.XXXXXX")"
tmp_b="$(mktemp -d "${TMPDIR:-/tmp}/codeberg-release-b.XXXXXX")"
trap 'rm -rf "$tmp_a" "$tmp_b"' EXIT HUP INT TERM

build_binary() {
  target="$1"
  output="$2"
  goos="${target%/*}"
  goarch="${target#*/}"

  GOOS="$goos" \
  GOARCH="$goarch" \
  CGO_ENABLED="$cgo_enabled" \
  GOTOOLCHAIN="$go_toolchain" \
  GOFLAGS="" \
  SOURCE_DATE_EPOCH="$source_date_epoch" \
    go build -trimpath -buildvcs=false -ldflags "$go_ldflags" -o "$output" "$main_package"
}

artifact_list="$outdir/.artifacts"
: > "$artifact_list"

printf 'Preparing Go release %s\n' "$version"
printf '  binary: %s\n' "$binary_name"
printf '  main:   %s\n' "$main_package"
printf '  out:    %s\n' "$outdir"
printf '\n'

for target in $targets; do
  goos="${target%/*}"
  goarch="${target#*/}"
  ext=""
  if [ "$goos" = "windows" ]; then
    ext=".exe"
  fi

  name="$binary_name-$version-$goos-$goarch"
  bin="$binary_name$ext"

  mkdir -p "$tmp_a/$target" "$tmp_b/$target"
  printf 'Building %s twice...\n' "$target"
  build_binary "$target" "$tmp_a/$target/$bin"
  build_binary "$target" "$tmp_b/$target/$bin"

  hash_a="$(hash_file "$tmp_a/$target/$bin" | awk '{print $1}')"
  hash_b="$(hash_file "$tmp_b/$target/$bin" | awk '{print $1}')"
  if [ "$hash_a" != "$hash_b" ]; then
    printf 'Reproducibility check failed for %s.\n' "$target" >&2
    printf '  first:  %s\n  second: %s\n' "$hash_a" "$hash_b" >&2
    exit 1
  fi
  printf '  reproducible binary hash: %s\n' "$hash_a"

  pkgdir="$outdir/$name"
  mkdir -p "$pkgdir"
  cp "$tmp_a/$target/$bin" "$pkgdir/"

  for extra in README.md README LICENSE LICENSE.md COPYING config.example.yaml; do
    if [ -f "$extra" ]; then
      cp "$extra" "$pkgdir/"
    fi
  done

  archive="$outdir/$name.tar.gz"
  if tar --version 2>/dev/null | grep -qi 'gnu tar'; then
    tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner -czf "$archive" -C "$outdir" "$name"
  else
    tar -czf "$archive" -C "$outdir" "$name"
  fi
  rm -rf "$pkgdir"
  printf '%s\n' "$(basename "$archive")" >> "$artifact_list"
done

printf '\nWriting SHA256SUMS...\n'
(
  cd "$outdir"
  : > SHA256SUMS
  while IFS= read -r artifact; do
    hash_file "$artifact" >> SHA256SUMS
  done < .artifacts
)

printf 'Signing SHA256SUMS...\n'
if [ -n "${GPG_KEY:-}" ]; then
  gpg --yes --armor --local-user "$GPG_KEY" --detach-sign -o "$outdir/SHA256SUMS.asc" "$outdir/SHA256SUMS"
else
  gpg --yes --armor --detach-sign -o "$outdir/SHA256SUMS.asc" "$outdir/SHA256SUMS"
fi

if [ "${SIGN_ARTIFACTS:-0}" = "1" ]; then
  printf 'Signing individual artifacts...\n'
  while IFS= read -r artifact; do
    if [ -n "${GPG_KEY:-}" ]; then
      gpg --yes --armor --local-user "$GPG_KEY" --detach-sign -o "$outdir/$artifact.asc" "$outdir/$artifact"
    else
      gpg --yes --armor --detach-sign -o "$outdir/$artifact.asc" "$outdir/$artifact"
    fi
  done < "$artifact_list"
fi

if [ "${EXPORT_GPG_PUBLIC_KEY:-0}" = "1" ]; then
  if [ -z "${GPG_KEY:-}" ]; then
    printf 'EXPORT_GPG_PUBLIC_KEY=1 requires GPG_KEY.\n' >&2
    exit 1
  fi
  gpg --armor --export "$GPG_KEY" > "$outdir/GPG-PUBLIC-KEY.asc"
fi

previous_tag="$(git describe --tags --abbrev=0 "$version^" 2>/dev/null || git describe --tags --abbrev=0 HEAD^ 2>/dev/null || true)"
notes="$outdir/RELEASE_NOTES.md"
{
  printf '# %s\n\n' "$version"
  printf 'Commit: `%s`\n\n' "$head_commit"
  printf '## Changes\n\n'
  if [ -n "$previous_tag" ]; then
    git log --pretty=format:'- %s (%h)' "$previous_tag..HEAD"
  else
    git log --pretty=format:'- %s (%h)' HEAD
  fi
  printf '\n\n## Verification\n\n'
  printf '```sh\n'
  printf 'gpg --verify SHA256SUMS.asc SHA256SUMS\n'
  printf 'sha256sum -c SHA256SUMS\n'
  printf '```\n'
} > "$notes"

{
  printf 'Release: %s\n' "$version"
  printf 'Commit: %s\n' "$head_commit"
  printf 'Source-Date-Epoch: %s\n' "$source_date_epoch"
  printf 'Targets: %s\n' "$targets"
  printf 'Main package: %s\n' "$main_package"
  printf '\nFiles:\n'
  for file in "$outdir"/*; do
    [ -f "$file" ] || continue
    printf '%s\n' "$(basename "$file")"
  done | sort | sed 's/^/- /'
} > "$outdir/RELEASE_MANIFEST.txt"

rm -f "$artifact_list"

printf '\nRelease files written to %s\n' "$outdir"
printf 'Verify locally with:\n'
printf '  cd %s && gpg --verify SHA256SUMS.asc SHA256SUMS && sha256sum -c SHA256SUMS\n' "$outdir"

