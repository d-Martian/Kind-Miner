#!/bin/sh
# Read-only reproducibility inventory and non-determinism smell scan.
# No writes, no network. Heuristic: findings are candidates to review against
# references/nondeterminism-catalog.md, not confirmed bugs.
#
# Usage: sh audit_reproducibility.sh [repo-root]   (defaults to .)
set -eu

root="${1:-.}"
root="${root%/}"

printf 'Reproducibility inventory for %s\n' "$root"
printf '================================================\n\n'

# ---- Git context -----------------------------------------------------------
if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  tag="$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo '?')"
  epoch="$(git -C "$root" log -1 --pretty=%ct 2>/dev/null || echo '?')"
  printf 'Git: describe=%s  SOURCE_DATE_EPOCH(commit)=%s\n\n' "$tag" "$epoch"
else
  printf 'Git: not a repository (toolchain/dep pinning still applies)\n\n'
fi

# ---- Candidate build/packaging/CI files (scan target) ----------------------
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
find "$root" \
  -type d \( -name .git -o -name .claude -o -name vendor -o -name node_modules -o -name dist -o -name build \) -prune -o \
  -type f \( \
      -name Makefile -o -name '*.mk' -o -name 'CMakeLists.txt' -o -name '*.cmake' \
   -o -name 'build.rs' -o -name 'setup.py' -o -name 'meson.build' \
   -o -name 'Dockerfile*' -o -name 'Containerfile' -o -name '*.gradle' -o -name 'pom.xml' \
   -o -path '*/scripts/*.sh' -o -path '*/.woodpecker*' \
   -o -path '*/.forgejo/*' -o -path '*/.github/workflows/*' \
  \) -print > "$tmp" 2>/dev/null || true

# grep_set PATTERN  -> matches across the candidate file list (file:line)
grep_set() {
  pat="$1"
  while IFS= read -r f; do
    [ -f "$f" ] && grep -nIE "$pat" "$f" 2>/dev/null | sed "s#^$root/#  #;s#^#  #"
  done < "$tmp"
}
# present PATTERN -> 0 if any candidate file matches
present() { grep_set "$1" | grep -q . 2>/dev/null; }

# ---- Ecosystems detected ---------------------------------------------------
printf 'Ecosystems detected:\n'
any=0
for pair in \
  'go.mod:Go' 'Cargo.toml:Rust' 'CMakeLists.txt:CMake/C++' 'Makefile:Make' \
  'package.json:Node' 'pyproject.toml:Python' 'setup.py:Python' \
  'pom.xml:Maven/JVM' 'build.gradle:Gradle/JVM' 'Dockerfile:Container' 'flake.nix:Nix'
do
  f="${pair%%:*}"; name="${pair#*:}"
  if [ -e "$root/$f" ] || find "$root" -maxdepth 3 -name "$f" 2>/dev/null | grep -q .; then
    printf '  - %s (%s)\n' "$name" "$f"; any=1
  fi
done
[ "$any" -eq 1 ] || printf '  (none of the common manifests found)\n'

# ---- Deterministic measures already present --------------------------------
printf '\nDeterminism measures already present:\n'
check() { # label  pattern
  if present "$2"; then printf '  [x] %s\n' "$1"; else printf '  [ ] %s\n' "$1"; fi
}
check 'SOURCE_DATE_EPOCH used'                 'SOURCE_DATE_EPOCH'
check 'Go -trimpath'                           '\-trimpath'
check 'Go -buildid= / -buildvcs=false'         '\-buildid=|\-buildvcs=false'
check 'Path remap (-ffile-prefix-map/--remap-path-prefix)' 'file-prefix-map|remap-path-prefix'
check 'Locale/timezone pinned (LC_ALL/TZ)'     'LC_ALL|(^|[^A-Za-z])TZ=UTC'
check 'Archive normalization (--sort/--mtime/strip-nondeterminism)' '\-\-sort|\-\-mtime|strip-nondeterminism'
check 'Toolchain pinned (GOTOOLCHAIN/rust-toolchain/JDK)' 'GOTOOLCHAIN|rust-toolchain|toolchain '

# ---- Dependency pinning / lockfiles ----------------------------------------
printf '\nDependency pinning (lockfiles):\n'
lock=0
for lf in go.sum Cargo.lock package-lock.json pnpm-lock.yaml yarn.lock \
          requirements.txt poetry.lock flake.lock vendor; do
  if [ -e "$root/$lf" ]; then printf '  - %s\n' "$lf"; lock=1; fi
done
[ "$lock" -eq 1 ] || printf '  (no lockfile/vendor dir found — inputs may float)\n'

# ---- Verification infrastructure -------------------------------------------
printf '\nVerification infrastructure:\n'
vi=0
for vf in REPRODUCIBLE.md SHA256SUMS SHA256SUMS.asc; do
  if find "$root" -maxdepth 2 -name "$vf" 2>/dev/null | grep -q .; then
    printf '  - %s present\n' "$vf"; vi=1
  fi
done
if present 'verify-repro|reproduce|diffoscope|reprotest'; then
  printf '  - rebuild/compare or diffoscope/reprotest referenced in build/CI\n'; vi=1
fi
[ "$vi" -eq 1 ] || printf '  (no rebuild-and-compare, SHA256SUMS, or REPRODUCIBLE.md found)\n'

# ---- Non-determinism smells (heuristic) ------------------------------------
printf '\nNon-determinism smells (review against the catalog):\n'
smell() { # label  pattern
  out="$(grep_set "$2" || true)"
  if [ -n "$out" ]; then printf '\n  %s\n%s\n' "$1" "$out"; fi
}
smell '§1 timestamps: build-time clock reads'      '\$\(date|[^A-Za-z]date \+|time\.Now\(\)|Date\.now\(\)|datetime\.now|__DATE__|__TIME__'
smell '§2/§6 host/path leakage'                    '\-march=native|hostname|whoami|uname \-n|describe.*--dirty'
smell '§3 unsorted file listing into output'       '\$\(wildcard|\bfind \b|os\.listdir|glob\.glob'
smell '§5 randomness / unstable ids'               'uuidgen|\$RANDOM|mktemp'
smell '§7 archive/package creation (check normalization)' '\btar \-|[^a-z]zip |gzip|npm pack|jar cf'
smell '§8 unpinned inputs'                         ':latest|go get |pip install|npm install|curl .*\|'

# Dockerfile base-image digest check
docks="$(grep_set '^[[:space:]]*FROM ' || true)"
if [ -n "$docks" ]; then
  printf '\n  §8 container base images (must be pinned by @sha256):\n%s\n' "$docks"
fi

printf '\n------------------------------------------------\n'
printf 'Next: baseline with scripts/rebuild_compare.sh, diagnose each smell via\n'
printf 'references/nondeterminism-catalog.md, apply references/language-recipes.md.\n'
