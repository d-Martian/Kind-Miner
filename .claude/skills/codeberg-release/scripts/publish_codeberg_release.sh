#!/bin/sh
set -eu

usage() {
  cat <<'USAGE'
Usage: publish_codeberg_release.sh OWNER REPO TAG OUTDIR

Creates a Codeberg/Forgejo release and uploads files from OUTDIR.
Creates a draft release by default.

Environment:
  CODEBERG_TOKEN       required API token
  CODEBERG_BASE_URL    default: https://codeberg.org
  RELEASE_NAME         default: TAG
  RELEASE_NOTES        default: OUTDIR/RELEASE_NOTES.md if present
  TARGET_COMMITISH     default: main
  DRAFT=0              publish immediately instead of draft
  PRERELEASE=1         mark as prerelease
USAGE
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

owner="${1:-}"
repo="${2:-}"
tag="${3:-}"
outdir="${4:-}"

if [ -z "$owner" ] || [ -z "$repo" ] || [ -z "$tag" ] || [ -z "$outdir" ]; then
  usage
  exit 2
fi

if [ -z "${CODEBERG_TOKEN:-}" ]; then
  printf 'CODEBERG_TOKEN is required.\n' >&2
  exit 2
fi

if [ ! -d "$outdir" ]; then
  printf 'Release directory not found: %s\n' "$outdir" >&2
  exit 1
fi

for tool in curl jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf '%s is required.\n' "$tool" >&2
    exit 1
  fi
done

if [ ! -f "$outdir/SHA256SUMS" ] || [ ! -f "$outdir/SHA256SUMS.asc" ]; then
  printf 'Expected SHA256SUMS and SHA256SUMS.asc in %s.\n' "$outdir" >&2
  exit 1
fi

bool_json() {
  case "${1:-}" in
    1|true|TRUE|yes|YES) printf 'true' ;;
    *) printf 'false' ;;
  esac
}

base="${CODEBERG_BASE_URL:-https://codeberg.org}"
api="${base%/}/api/v1"
name="${RELEASE_NAME:-$tag}"
target="${TARGET_COMMITISH:-main}"
draft="$(bool_json "${DRAFT:-1}")"
prerelease="$(bool_json "${PRERELEASE:-0}")"

notes_file="${RELEASE_NOTES:-}"
if [ -z "$notes_file" ] && [ -f "$outdir/RELEASE_NOTES.md" ]; then
  notes_file="$outdir/RELEASE_NOTES.md"
fi

body=""
if [ -n "$notes_file" ]; then
  if [ ! -f "$notes_file" ]; then
    printf 'Release notes file not found: %s\n' "$notes_file" >&2
    exit 1
  fi
  body="$(cat "$notes_file")"
fi

payload="$(jq -n \
  --arg tag "$tag" \
  --arg target "$target" \
  --arg name "$name" \
  --arg body "$body" \
  --argjson draft "$draft" \
  --argjson prerelease "$prerelease" \
  '{tag_name:$tag,target_commitish:$target,name:$name,body:$body,draft:$draft,prerelease:$prerelease}')"

printf 'Creating release %s/%s %s (draft=%s)...\n' "$owner" "$repo" "$tag" "$draft"
response="$(curl --fail --silent --show-error \
  -H "Authorization: token $CODEBERG_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$payload" \
  "$api/repos/$owner/$repo/releases")"

release_id="$(printf '%s\n' "$response" | jq -r '.id // empty')"
release_url="$(printf '%s\n' "$response" | jq -r '.html_url // empty')"

if [ -z "$release_id" ]; then
  printf 'Release was created but no id was returned.\n' >&2
  printf '%s\n' "$response" >&2
  exit 1
fi

for file in "$outdir"/*; do
  [ -f "$file" ] || continue
  filename="$(basename "$file")"
  if [ "$filename" = "RELEASE_NOTES.md" ]; then
    continue
  fi
  encoded_name="$(jq -rn --arg v "$filename" '$v|@uri')"
  printf 'Uploading %s...\n' "$filename"
  curl --fail --silent --show-error \
    -H "Authorization: token $CODEBERG_TOKEN" \
    -F "attachment=@$file" \
    "$api/repos/$owner/$repo/releases/$release_id/assets?name=$encoded_name" >/dev/null
done

printf '\nRelease created.\n'
if [ -n "$release_url" ]; then
  printf 'Review: %s\n' "$release_url"
else
  printf 'Review in Codeberg releases for %s/%s.\n' "$owner" "$repo"
fi

